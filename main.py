import logging
from urllib.parse import urlparse
import socket
import time
import threading
import atexit
import os

from opentelemetry import _logs
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.exporter.otlp.proto.http._log_exporter import (
    OTLPLogExporter as OTLPHTTPLogExporter,
)


class SafeOTLPLogExporter:
    """
    Wraps OTLPLogExporter and implements exponential backoff on failures.
    - initial backoff: 1s
    - doubles on each failure up to max_backoff
    - exporter will attempt again after the backoff interval
    - avoids using the logging subsystem to prevent recursive exports
    """

    def __init__(
        self,
        *args,
        exporter_cls=None,
        endpoint: str | None = None,
        initial_backoff: float = 1.0,
        max_backoff: float = 300.0,
        **kwargs,
    ):
        """
        If exporter_cls is provided, instantiate that exporter.
        Otherwise try to pick based on endpoint.scheme:
            - http/https -> OTLP HTTP exporter
            - grpc/grpcs (or no http scheme) -> OTLP gRPC exporter
        """
        # choose exporter class
        if exporter_cls is None:
            # allow explicit protocol via env var (http/protobuf, http/json, grpc)
            proto_env = os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "").lower().strip()
            if proto_env in ("grpc",):
                want_grpc = True
            elif proto_env.startswith("http") or proto_env in ("http/protobuf", "http/json", "http"):
                want_grpc = False
            else:
                # fall back to endpoint scheme if available
                scheme = urlparse(endpoint).scheme.lower() if endpoint else None
                want_grpc = scheme not in ("http", "https")

            if not want_grpc:
                exporter_cls = OTLPHTTPLogExporter
            else:
                # lazy-import gRPC exporter; if that fails, fall back to HTTP
                try:
                    from opentelemetry.exporter.otlp.proto.grpc._log_exporter import (
                        OTLPLogExporter as OTLPGrpcLogExporter,
                    )

                    exporter_cls = OTLPGrpcLogExporter
                except Exception as exc:
                    import sys

                    print(
                        "gRPC OTLP exporter unavailable; falling back to HTTP exporter. "
                        "Install system libstdc++ (e.g. apt install libstdc++6) to enable gRPC. "
                        f"Import error: {exc}",
                        file=sys.stderr,
                    )
                    exporter_cls = OTLPHTTPLogExporter

        # instantiate the chosen exporter; caller may pass endpoint in kwargs/args
        self._inner = exporter_cls(*args, **kwargs)
        self._lock = threading.Lock()
        self._reported = False  # ensure we print the first failure only
        self._backoff_initial = float(initial_backoff)
        self._backoff = float(initial_backoff)
        self._max_backoff = float(max_backoff)
        self._next_try = 0.0  # epoch when next attempt is allowed

    def _report_once(self, message: str, exc: Exception | None = None):
        if self._reported:
            return
        self._reported = True
        import sys, traceback

        print(message, file=sys.stderr)
        if exc is not None:
            traceback.print_exception(
                type(exc), exc, exc.__traceback__, file=sys.stderr
            )

    def _report_retry(self, wait_seconds: float):
        # safe repeated message about scheduled retry (not using logging)
        import sys

        print(f"OTLP exporter will retry in {wait_seconds:.1f}s", file=sys.stderr)

    def export(self, records):
        now = time.time()
        with self._lock:
            if now < self._next_try:
                # within backoff window, skip export
                return None
        try:
            result = self._inner.export(records)
        except Exception as exc:
            # schedule next retry with exponential backoff
            with self._lock:
                # report the first failure and show the backoff schedule
                self._report_once(
                    "OTLP export failed; entering backoff. Disabling immediate exports.",
                    exc,
                )
                self._report_retry(self._backoff)
                self._next_try = time.time() + self._backoff
                self._backoff = min(self._backoff * 2.0, self._max_backoff)
            return None
        else:
            # success: reset backoff so future failures start from initial interval
            with self._lock:
                self._backoff = self._backoff_initial
                self._next_try = 0.0
            return result

    def shutdown(self):
        try:
            return getattr(self._inner, "shutdown", lambda: None)()
        except Exception as exc:
            self._report_once("Exception while shutting down OTLP exporter", exc)

    def force_flush(self, timeout_millis: int = 30000):
        try:
            return getattr(self._inner, "force_flush", lambda *a, **k: True)(
                timeout_millis
            )
        except Exception as exc:
            self._report_once("Exception during force_flush", exc)
            return False


def _is_endpoint_reachable(url: str, timeout: float = 0.5) -> bool:
    """Fast TCP check for the endpoint host:port (does not perform HTTP)."""
    parsed = urlparse(url)
    host = parsed.hostname or "localhost"
    port = parsed.port or (4318 if parsed.scheme in ("http", "https") else None)
    if port is None:
        return False
    try:
        with socket.create_connection((host, port), timeout):
            return True
    except Exception:
        return False


def _start_otlp_prober(
    provider, endpoint: str, initial_backoff: float = 5.0, max_backoff: float = 300.0
):
    """
    Background thread: probe endpoint with exponential backoff and attach the
    SafeOTLPLogExporter once reachable. Prints status to stderr for each probe
    attempt, failure, scheduled retry, and successful attach.
    """

    def _probe():
        backoff = float(initial_backoff)
        attempt = 0
        import sys
        import time as _time

        print(f"Starting OTLP prober for {endpoint}", file=sys.stderr)
        while True:
            attempt += 1
            try:
                if _is_endpoint_reachable(endpoint, timeout=0.5):
                    try:
                        exporter = SafeOTLPLogExporter(
                            endpoint=endpoint,
                            initial_backoff=initial_backoff,
                            max_backoff=max_backoff,
                        )
                        provider.add_log_record_processor(
                            BatchLogRecordProcessor(exporter)
                        )
                        print(
                            f"OTLP exporter attached to {endpoint} (after {attempt} attempts)",
                            file=sys.stderr,
                        )
                        break
                    except Exception as exc:
                        print(f"Failed to attach OTLP exporter: {exc}", file=sys.stderr)
                        # treat as a transient failure and continue retrying
                else:
                    print(
                        f"OTLP endpoint {endpoint} not reachable (attempt {attempt}); retrying in {backoff:.1f}s",
                        file=sys.stderr,
                    )
            except Exception as exc:
                # unexpected error in prober itself
                print(
                    f"OTLP prober error on attempt {attempt}: {exc}; retrying in {backoff:.1f}s",
                    file=sys.stderr,
                )
            _time.sleep(backoff)
            backoff = min(backoff * 2.0, max_backoff)

    t = threading.Thread(target=_probe, daemon=True, name="otlp-prober")
    t.start()


def configure_logging():
    #
    # ---- 1. STDOUT LOGGING (human-friendly) ----
    #
    stdout_handler = logging.StreamHandler()
    stdout_handler.setLevel(logging.WARNING)  # <-- stdout only WARN+
    stdout_handler.setFormatter(
        logging.Formatter(
            fmt="[STDOUT][{levelname}] {message}",
            style="{",
        )
    )

    #
    # ---- 2. OTel LOGGING PIPELINE (OTLP exporter, INFO+) ----
    #
    provider = LoggerProvider()
    _logs.set_logger_provider(provider)

    # Start a background prober that will attach the exporter when reachable.
    endpoint = "grpc://localhost:4318/v1/logs"
    _start_otlp_prober(provider, endpoint, initial_backoff=2.0, max_backoff=300.0)

    otel_handler = LoggingHandler()
    otel_handler.setLevel(logging.INFO)  # <-- OTel only INFO+

    #
    # ---- Attach handlers to the root logger ----
    #
    root = logging.getLogger()
    root.setLevel(logging.DEBUG)  # root must be low enough
    root.addHandler(stdout_handler)
    root.addHandler(otel_handler)

    return provider


def main():
    provider = configure_logging()
    log = logging.getLogger("demo")

    try:
        for i in range(300):
            print(f"--- Iteration {i} ---")
            log.debug("iteration %d - debug will not go anywhere", i)
            log.info("iteration %d - info goes to OTLP only", i)
            log.warning("iteration %d - warning goes to stdout + OTLP", i)
            log.error("iteration %d - error goes to stdout + OTLP", i)
            time.sleep(1)
    finally:
        # Ensure processors call exporter.force_flush() and exporter.shutdown()
        try:
            provider.shutdown()
        except Exception:
            pass


if __name__ == "__main__":
    main()

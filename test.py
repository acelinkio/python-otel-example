import os
from urllib.parse import urlparse
from opentelemetry.exporter.otlp.proto.http._log_exporter import (
    OTLPLogExporter as HttpLogExporter,
)
from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
    OTLPMetricExporter as HttpMetricExporter,
)
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter as HttpSpanExporter,
)

import sys
import time
import threading
import traceback
from typing import Optional, Type, Any
import socket

proto_env = os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "").lower().strip()
endpoint_env = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()
print(f"Using OTEL_EXPORTER_OTLP_PROTOCOL={proto_env} and OTEL_EXPORTER_OTLP_ENDPOINT={endpoint_env}")

# parse OTEL_EXPORTER VARIABLES
if proto_env in ("grpc",):
    want_grpc = True
elif proto_env.startswith("http") or proto_env in ("http/protobuf", "http/json", "http"):
    want_grpc = False
else:
    # fall back to endpoint scheme if available
    scheme = urlparse(endpoint_env).scheme.lower() if endpoint_env else None
    want_grpc = scheme not in ("http", "https")

# use appropriate exporter
if not want_grpc:
  LogExporterClass = HttpLogExporter
  MetricsExporterClass = HttpMetricExporter
  TracesExporterClass = HttpSpanExporter
else:
    # lazy-import gRPC exporter; if that fails, fall back to HTTP
    try:
        from opentelemetry.exporter.otlp.proto.grpc._log_exporter import (
            OTLPLogExporter as GrpcLogExporter,
        )
        from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
          OTLPMetricExporter as GrpcMetricExporter,
        )
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
          OTLPSpanExporter as GrpcSpanExporter,
        )
        LogExporterClass = GrpcLogExporter
        MetricsExporterClass = GrpcMetricExporter
        TracesExporterClass = GrpcSpanExporter
    except Exception as exc:
        print(
            "gRPC OTLP exporter unavailable; falling back to HTTP exporter. "
            "Install system libstdc++ (e.g. apt install libstdc++6) to enable gRPC. "
            f"Import error: {exc}",
            file=sys.stderr,
        )
        LogExporterClass = HttpLogExporter
        MetricsExporterClass = HttpMetricExporter
        TracesExporterClass = HttpSpanExporter

# --- Modular design: use module-level classes, remove resolver -----------------

# compact mapping chosen at import time; tests can monkeypatch the three module vars.
EXPORTER_CLASSES = {
    "logs": LogExporterClass,
    "traces": TracesExporterClass,
    "metrics": MetricsExporterClass,
}

def make_safe_exporter(signal: str, *args, exporter_cls: Optional[Type[Any]] = None, **kwargs) -> "SafeOTLPExporter":
    """Create a SafeOTLPExporter for the named signal using the module-level defaults."""
    if exporter_cls is None:
        try:
            exporter_cls = EXPORTER_CLASSES[signal]
        except KeyError:
            raise ValueError(f"Unknown signal '{signal}' (expected one of {list(EXPORTER_CLASSES.keys())})")
    return SafeOTLPExporter(*args, exporter_cls=exporter_cls, **kwargs)

class SafeOTLPExporter:
    """
    Generic wrapper around an OTLP exporter instance that implements exponential
    backoff on export failures and avoids using logging for internal messages.

    NOTE: simplified: this wrapper no longer accepts a 'signal' string.  Pass an
    exporter class explicitly, or use the convenience factory functions below
    which use DEFAULT_EXPORTER_CLASS.
    """

    def __init__(
        self,
        *args,
        exporter_cls: Optional[Type[Any]] = None,
        endpoint: Optional[str] = None,
        initial_backoff: float = 1.0,
        max_backoff: float = 300.0,
        **kwargs,
    ):
        # require an exporter class (keeps the wrapper signal-agnostic / simple)
        if exporter_cls is None:
            raise ValueError(
                "exporter_cls is required. Use make_safe_log_exporter / "
                "make_safe_trace_exporter / make_safe_metric_exporter or pass exporter_cls."
            )

        # instantiate the exporter (caller can pass endpoint/credentials via kwargs)
        self._inner = exporter_cls(*args, **kwargs)
        self._lock = threading.Lock()
        self._reported = False
        self._backoff_initial = float(initial_backoff)
        self._backoff = float(initial_backoff)
        self._max_backoff = float(max_backoff)
        self._next_try = 0.0

    def _report_once(self, message: str, exc: Optional[BaseException] = None):
        if self._reported:
            return
        self._reported = True
        print(message, file=sys.stderr)
        if exc is not None:
            traceback.print_exception(type(exc), exc, exc.__traceback__, file=sys.stderr)

    def _report_retry(self, wait_seconds: float):
        print(f"OTLP exporter will retry in {wait_seconds:.1f}s", file=sys.stderr)

    def export(self, records) -> Optional[Any]:
        now = time.time()
        with self._lock:
            if now < self._next_try:
                # within backoff window, skip
                return None
        try:
            return getattr(self._inner, "export")(records)
        except Exception as exc:
            with self._lock:
                self._report_once("OTLP export failed; entering backoff. Disabling immediate exports.", exc)
                self._report_retry(self._backoff)
                self._next_try = time.time() + self._backoff
                self._backoff = min(self._backoff * 2.0, self._max_backoff)
            return None

    def shutdown(self):
        try:
            return getattr(self._inner, "shutdown", lambda: None)()
        except Exception as exc:
            self._report_once("Exception while shutting down OTLP exporter", exc)

    def force_flush(self, timeout_millis: int = 30000):
        try:
            return getattr(self._inner, "force_flush", lambda *a, **k: True)(timeout_millis)
        except Exception as exc:
            self._report_once("Exception during force_flush", exc)
            return False

# --- end SafeOTLPExporter -----------------------------------------------------

def main():
    # Demonstrate resolving exporters for each signal using the module-level mapping.
    try:
        signals = ["logs", "traces", "metrics"]
        resolved = {}

        for s in signals:
            try:
                wrapper = make_safe_exporter(s, endpoint=endpoint_env or None)
            except Exception as e:
                print(f"Could not create exporter for {s}: {e}", file=sys.stderr)
                continue
            cls = wrapper._inner.__class__
            print(f"Resolved {s} exporter: {cls.__module__}.{cls.__name__}")
            resolved[s] = wrapper

        for i in range(50):
            print(f"--- Iteration {i} ---")
            time.sleep(0.2)

    finally:
        # tear down: call shutdown/force_flush on any wrappers we created
        for w in resolved.values():
            try:
                w.force_flush()
                w.shutdown()
            except Exception:
                pass


# thin convenience aliases (optional)
make_safe_log_exporter = lambda *a, exporter_cls=None, **kw: make_safe_exporter("logs", *a, exporter_cls=exporter_cls, **kw)
make_safe_trace_exporter = lambda *a, exporter_cls=None, **kw: make_safe_exporter("traces", *a, exporter_cls=exporter_cls, **kw)
make_safe_metric_exporter = lambda *a, exporter_cls=None, **kw: make_safe_exporter("metrics", *a, exporter_cls=exporter_cls, **kw)


if __name__ == "__main__":
    main()
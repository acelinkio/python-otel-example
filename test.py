import os
import sys
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


# new imports
import logging
from opentelemetry import trace, metrics, _logs
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource

proto_env = os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "").lower().strip()
endpoint_env = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()
print(
    f"Using OTEL_EXPORTER_OTLP_PROTOCOL={proto_env} and OTEL_EXPORTER_OTLP_ENDPOINT={endpoint_env}"
)

# parse OTEL_EXPORTER VARIABLES
if proto_env in ("grpc",):
    want_grpc = True
elif proto_env.startswith("http") or proto_env in (
    "http/protobuf",
    "http/json",
    "http",
):
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
    # lazy-import gRPC exporter; if that fails, fail hard (no HTTP fallback)
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
        # fail immediately — gRPC is required in this deployment
        raise RuntimeError(
            "gRPC OTLP exporter imports failed; gRPC is required (no HTTP fallback). "
            "Install system libstdc++ (e.g. apt install libstdc++6) to enable gRPC.."
        ) from exc


# --- NEW: configure SDKs (traces, metrics, logs) ---
def configure_otel(endpoint: str | None, protocol: str | None):
    """
    Configure SDK providers without programmatically setting exporter endpoints.

    This version deliberately does NOT pass an `endpoint` argument to the
    HTTP exporters. Exporter behaviour will be driven by the standard
    environment variables (OTEL_EXPORTER_OTLP_ENDPOINT or the
    per-signal OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT) or the
    exporters' built-in defaults. Use per-signal env vars in environments
    that require explicit /v1/<signal> paths.
    """
    resource = Resource.create(
        {"service.name": os.getenv("OTEL_SERVICE_NAME", "testpythonapp")}
    )

    # Traces
    tracer_provider = TracerProvider(resource=resource)
    trace.set_tracer_provider(tracer_provider)
    # Do not pass endpoint here; let exporter read env vars / defaults
    traces_exporter = TracesExporterClass()
    tracer_provider.add_span_processor(BatchSpanProcessor(traces_exporter))

    # Metrics
    metrics_exporter = MetricsExporterClass()
    metric_reader = PeriodicExportingMetricReader(metrics_exporter)
    meter_provider = MeterProvider(resource=resource, metric_readers=[metric_reader])
    metrics.set_meter_provider(meter_provider)

    # Logs
    logger_provider = LoggerProvider(resource=resource)
    _logs.set_logger_provider(logger_provider)
    log_exporter = LogExporterClass()
    logger_provider.add_log_record_processor(BatchLogRecordProcessor(log_exporter))

    # Configure python logging
    root_logger = logging.getLogger()
    # keep logger permissive; handlers filter emission
    root_logger.setLevel(logging.DEBUG)

    # Wire python logging to OTel logs (INFO+)
    py_handler = LoggingHandler(level=logging.INFO)
    root_logger.addHandler(py_handler)

    # also print logs to stdout/stderr for local visibility (WARNING+)
    import sys

    stream_handler = logging.StreamHandler(sys.stdout)
    stream_handler.setLevel(logging.WARNING)
    root_logger.addHandler(stream_handler)


    return tracer_provider, meter_provider, logger_provider


def main():
    # initialize SDKs to send to OTLP endpoint
    providers = configure_otel(endpoint_env or None, proto_env or None)

    log = logging.getLogger(__name__)
    tracer = trace.get_tracer(__name__)
    meter = metrics.get_meter(__name__)
    counter = meter.create_counter("example.counter", description="Example counter")

    try:
        import time
        for i in range(500):
            print(f"--- Iteration {i} ---")
            # example span
            with tracer.start_as_current_span("iteration-span") as span:
                span.set_attribute("iteration", i)
                # increment metric
                counter.add(1, {"iteration": str(i)})
                # logs (will be exported via OTel + python handlers)
                log.info("iteration %d - info goes to OTLP", i)
                log.warning("iteration %d - warning goes to stdout + OTLP", i)
            time.sleep(1)
    finally:
        # Ensure processors call shutdown()
        try:
            for p in providers or ():
                try:
                    p.shutdown()
                except Exception:
                    pass
            print("shutdown complete")
        except Exception:
            pass


if __name__ == "__main__":
    main()
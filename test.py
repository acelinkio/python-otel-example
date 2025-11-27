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
        import sys
        print(
            "gRPC OTLP exporter unavailable; falling back to HTTP exporter. "
            "Install system libstdc++ (e.g. apt install libstdc++6) to enable gRPC. "
            f"Import error: {exc}",
            file=sys.stderr,
        )
        LogExporterClass = HttpLogExporter
        MetricsExporterClass = HttpMetricExporter
        TracesExporterClass = HttpSpanExporter



def main():
    try:
        for i in range(5):
            print(f"--- Iteration {i} ---")
    finally:
        # Ensure processors call exporter.force_flush() and exporter.shutdown()
        try:
            print("test123")
        except Exception:
            pass


if __name__ == "__main__":
    main()
def setup_otelproviders() -> tuple[object, object, object]:
    import os
    if not (os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()):
        print("OTEL_EXPORTER_OTLP_ENDPOINT not specified, skipping otel exporter setup.")
        return None, None, None
    elif os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "").lower().strip() == "grpc":
        # lazy-import gRPC exporter; if that fails, fail hard
        try:
            from opentelemetry.exporter.otlp.proto.grpc._log_exporter import (
                OTLPLogExporter,
            )
            from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
                OTLPMetricExporter,
            )
            from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import (
                OTLPSpanExporter,
            )
        except Exception as exc:
            raise RuntimeError(
                "gRPC OTLP exporter imports failed. Install the necessary packages / system libs."
                "Try installing libstdc++ (e.g. apt install libstdc++6) to enable gRPC."
            ) from exc
        else:
            print("Using gRPC libs for OpenTelemetry exporter.")
    else:
        # HTTP exporter path
        try:
            from opentelemetry.exporter.otlp.proto.http._log_exporter import (
                OTLPLogExporter,
            )
            from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
                OTLPMetricExporter,
            )
            from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
                OTLPSpanExporter,
            )
        except Exception as exc:
            raise RuntimeError(
                "HTTP OTLP exporter imports failed. Install the necessary packages / system libs."
            ) from exc
        else:
            print("Using HTTP libs for OpenTelemetry exporter.")

    from opentelemetry import trace, metrics, _logs
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.sdk.metrics import MeterProvider
    from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
    from opentelemetry.sdk._logs import LoggerProvider
    from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
    from opentelemetry.sdk.resources import Resource

    resource = Resource.create(
        {"service.name": os.getenv("OTEL_SERVICE_NAME", "testpythonapp")}
    )

    # Traces
    tracer_provider = TracerProvider(resource=resource)
    tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    trace.set_tracer_provider(tracer_provider)

    # Metrics
    meter_provider = MeterProvider(
        resource=resource,
        metric_readers=[PeriodicExportingMetricReader(OTLPMetricExporter())],
    )
    metrics.set_meter_provider(meter_provider)

    # Logs
    logger_provider = LoggerProvider(resource=resource)
    logger_provider.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter()))
    _logs.set_logger_provider(logger_provider)

    return tracer_provider, meter_provider, logger_provider

def setup_logging():
    import sys
    import logging
    from opentelemetry.sdk._logs import LoggingHandler
    
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)
    root_logger.addHandler(LoggingHandler())

    stdout_handler = logging.StreamHandler(sys.stdout)
    stdout_handler.setLevel(logging.WARNING)
    stdout_handler.setFormatter(
        logging.Formatter(
            fmt="[STDOUT][{levelname}][{name}] {message}",
            style="{",
        )
    )
    root_logger.addHandler(stdout_handler)

def main():
    try:
        import logging
        from opentelemetry import trace, metrics

        tracer_provider, meter_provider, logger_provider = setup_otelproviders()
        setup_logging()

        log = logging.getLogger(__name__)
        tracer = trace.get_tracer(__name__)
        meter = metrics.get_meter(__name__)

        counter = meter.create_counter("example.counter", description="Example counter")

        import time
        for i in range(500):
            print(f"--- Iteration {i} ---")
            # example span
            with tracer.start_as_current_span("iteration-span") as span:
                span.set_attribute("iteration", i)
                # increment metric
                counter.add(1, {"iteration": str(i)})
                try:
                    # simulate an error for demonstration
                    if i == 5:
                        1 / 0
                    # normal logs
                    log.info("iteration %d - info goes to OTLP", i)
                    log.warning("iteration %d - warning goes to stdout + OTLP", i)
                except Exception:
                    # log.exception() includes exc_info=True and prints the traceback
                    log.exception("iteration %d failed with exception", i)
            time.sleep(1)
    finally:
        if tracer_provider is not None:
            try:
                tracer_provider.shutdown()
            except Exception:
                pass
        if meter_provider is not None:
            try:
                meter_provider.shutdown()
            except Exception:
                pass
        if logger_provider is not None:
            try:
                logger_provider.shutdown()
            except Exception:
                pass

if __name__ == "__main__":
    main()

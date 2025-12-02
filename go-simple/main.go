package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	otellogs "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	requestCounter metric.Int64Counter
	petalHistogram metric.Int64Histogram
	otelLogger     otellogs.Logger
)

// new: slog handler that forwards to the OpenTelemetry SDK logger
type otelSlogHandler struct {
	logger otellogs.Logger
}

func (h *otelSlogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *otelSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	var rec otellogs.Record

	// Record timestamp (r.Time is a time.Time field)
	rec.SetTimestamp(r.Time)

	// map slog level -> otel severity
	switch r.Level {
	case slog.LevelError:
		rec.SetSeverity(otellogs.SeverityError)
	case slog.LevelWarn:
		rec.SetSeverity(otellogs.SeverityWarn)
	case slog.LevelInfo:
		rec.SetSeverity(otellogs.SeverityInfo)
	case slog.LevelDebug:
		rec.SetSeverity(otellogs.SeverityDebug)
	default:
		// fallback to Info if unspecified
		rec.SetSeverity(otellogs.SeverityInfo)
	}

	rec.SetBody(otellogs.StringValue(r.Message))

	// copy attributes from slog record (use the Attrs iterator)
	r.Attrs(func(a slog.Attr) bool {
		rec.AddAttributes(otellogs.String(a.Key, fmt.Sprint(a.Value)))
		return true
	})

	// if a span is in ctx, include trace/span ids
	if s := trace.SpanFromContext(ctx); s != nil {
		sc := s.SpanContext()
		if sc.IsValid() {
			rec.AddAttributes(
				otellogs.String("trace_id", sc.TraceID().String()),
				otellogs.String("span_id", sc.SpanID().String()),
			)
		}
	}

	h.logger.Emit(ctx, rec)
	return nil
}

func (h *otelSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *otelSlogHandler) WithGroup(_ string) slog.Handler       { return h }

func initObservability() func() {
	ctx := context.Background()

	// Create resource - uses OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_* env vars
	res, err := resource.New(ctx,
		resource.WithFromEnv(),   // Reads OTEL_RESOURCE_ATTRIBUTES, OTEL_SERVICE_NAME, etc.
		resource.WithAttributes(
			semconv.ServiceNameKey.String("sakura-service"),     // Default if not set via env
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		log.Fatalf("failed to create resource: %v", err)
	}

	// Initialize tracing - respects OTEL_TRACES_EXPORTER env var
	var traceExporter sdktrace.SpanExporter
	tracesExporter := os.Getenv("OTEL_TRACES_EXPORTER")
	if tracesExporter == "stdout" {
		traceExporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else {
		// Default to OTLP - uses OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
		traceExporter, err = otlptracegrpc.New(ctx)
	}
	if err != nil {
		log.Fatalf("failed to create trace exporter: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Initialize metrics - respects OTEL_METRICS_EXPORTER env var
	var metricExporter sdkmetric.Exporter
	metricsExporter := os.Getenv("OTEL_METRICS_EXPORTER")
	if metricsExporter == "stdout" {
		metricExporter, err = stdoutmetric.New(stdoutmetric.WithPrettyPrint())
	} else {
		// Default to OTLP - uses OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
		metricExporter, err = otlpmetricgrpc.New(ctx)
	}
	if err != nil {
		log.Fatalf("failed to create metric exporter: %v", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(5*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Initialize logging - respects OTEL_LOGS_EXPORTER env var
	var logExporter sdklog.Exporter
	logsExporter := os.Getenv("OTEL_LOGS_EXPORTER")
	if logsExporter == "stdout" {
		logExporter, err = stdoutlog.New(stdoutlog.WithPrettyPrint())
	} else {
		// Default to OTLP - uses OTEL_EXPORTER_OTLP_LOGS_ENDPOINT
		logExporter, err = otlploggrpc.New(ctx)
	}
	if err != nil {
		log.Fatalf("failed to create log exporter: %v", err)
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)

	// Create OpenTelemetry logger
	otelLogger = loggerProvider.Logger("sakura-service")

	// install slog handler that forwards to the otel logger
	slogHandler := &otelSlogHandler{logger: otelLogger}
	// set the global default slog logger to our handler
	slog.SetDefault(slog.New(slogHandler))

	// Create metrics instruments
	meter := otel.Meter("sakura-service")
	requestCounter, err = meter.Int64Counter(
		"sakura_requests_total",
		metric.WithDescription("Total number of sakura requests"),
	)
	if err != nil {
		log.Fatalf("failed to create counter: %v", err)
	}

	petalHistogram, err = meter.Int64Histogram(
		"sakura_petals_count",
		metric.WithDescription("Distribution of petal counts"),
	)
	if err != nil {
		log.Fatalf("failed to create histogram: %v", err)
	}

	return func() {
		shutdownCtx := context.Background()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
		if err := mp.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
		}
		if err := loggerProvider.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down logger provider: %v", err)
		}
	}
}

func main() {
	shutdown := initObservability()
	defer shutdown()

	http.HandleFunc("/sakura", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Create span with attributes
		_, span := otel.Tracer("sakura").Start(ctx, "GetSakuraStats")
		defer span.End()

		// Generate sakura data
		stage := []string{"early", "peak", "falling"}[rand.Intn(3)]
		petals := 100 + rand.Intn(500)

		// Add span attributes
		span.SetAttributes(
			attribute.String("sakura.stage", stage),
			attribute.Int("sakura.petals", petals),
		)

		// Record metrics
		requestCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("stage", stage),
		))
		petalHistogram.Record(ctx, int64(petals), metric.WithAttributes(
			attribute.String("stage", stage),
		))
		
		// Send structured logs via OpenTelemetry
		// Use slog (forwarded to OTLP by otelSlogHandler)
		slog.InfoContext(ctx, "🌸 Serving sakura stats",
			"sakura.stage", stage,
			"sakura.petals", petals,
			"service.name", "sakura-service",
			"trace_id", span.SpanContext().TraceID().String(),
			"span_id", span.SpanContext().SpanID().String(),
		)
		
		fmt.Fprintf(w, `{"stage": "%s", "petals": %d}`, stage, petals)
	})

	log.Println("🌸 Sakura Stats API running at http://localhost:8021/sakura")
	log.Fatal(http.ListenAndServe(":8021", nil))
}
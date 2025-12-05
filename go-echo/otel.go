package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	otel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	logotel "go.opentelemetry.io/otel/log"
	logotelglobal "go.opentelemetry.io/otel/log/global"
	logotelnoop "go.opentelemetry.io/otel/log/noop"
	logsdk "go.opentelemetry.io/otel/sdk/log"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	metricotel "go.opentelemetry.io/otel/metric"
	metricotelnoop "go.opentelemetry.io/otel/metric/noop"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	traceotel "go.opentelemetry.io/otel/trace"
	traceotelnoop "go.opentelemetry.io/otel/trace/noop"
)

func SetupOtel(ctx context.Context) (func(context.Context) error, error) {
	var le logsdk.Exporter
	var me metricsdk.Exporter
	var te tracesdk.SpanExporter
	var err error

	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = "example"
	}

	res, err := resource.New(
		ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	slog.Info("Configuring OTEL")
	switch {
	case endpoint == "":
		slog.Info("Using OLTP exporter type", "type", "noop")
		le = nil
		me = nil
		te = nil
	case strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))) == "grpc":
		slog.Info("Using OLTP exporter type", "type", "grpc")
		le, err = otlploggrpc.New(ctx)
		me, err = otlpmetricgrpc.New(ctx)
		te, err = otlptracegrpc.New(ctx)
	default:
		slog.Info("Using OLTP exporter type", "type", "http")
		le, err = otlploghttp.New(ctx)
		me, err = otlpmetrichttp.New(ctx)
		te, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}

	// default to noop providers
	var lp logotel.LoggerProvider = logotelnoop.NewLoggerProvider()
	var mp metricotel.MeterProvider = metricotelnoop.NewMeterProvider()
	var tp traceotel.TracerProvider = traceotelnoop.NewTracerProvider()

	// use exporter if configured
	if le != nil {
		lp = logsdk.NewLoggerProvider(
			logsdk.WithProcessor(logsdk.NewBatchProcessor(le)),
			logsdk.WithResource(res),
		)
	}

	if me != nil {
		mp = metricsdk.NewMeterProvider(
			metricsdk.WithReader(
				metricsdk.NewPeriodicReader(me),
			),
			metricsdk.WithResource(res),
		)
	}

	if te != nil {
		tp = tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(te),
			tracesdk.WithResource(res),
		)
	}

	// set providers
	logotelglobal.SetLoggerProvider(lp)
	otel.SetMeterProvider(mp)
	otel.SetTracerProvider(tp)

	// configure shutting down
	log_shutdown := func(ctx context.Context) error {
		if otelprovider, ok := lp.(*logsdk.LoggerProvider); ok && otelprovider != nil {
			return otelprovider.Shutdown(ctx)
		}
		return nil
	}

	metric_shutdown := func(ctx context.Context) error {
		if otelprovider, ok := mp.(*metricsdk.MeterProvider); ok && otelprovider != nil {
			return otelprovider.Shutdown(ctx)
		}
		return nil
	}

	trace_shutdown := func(ctx context.Context) error {
		if otelprovider, ok := tp.(*tracesdk.TracerProvider); ok && otelprovider != nil {
			return otelprovider.Shutdown(ctx)
		}
		return nil
	}

	return func(ctx context.Context) error {
		slog.Info("Shutting down OTEL")

		// run shutdowns and collect errors
		var errs []error
		if err := trace_shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := metric_shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := log_shutdown(ctx); err != nil {
			errs = append(errs, err)
		}

		if len(errs) == 0 {
			return nil
		}
		return errors.Join(errs...)
	}, nil
}

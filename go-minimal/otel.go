package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	noopsdklog "go.opentelemetry.io/otel/log/noop"
)

// InitOtelLogging creates the OTLP log exporter and returns a shutdown function.
// It chooses gRPC or HTTP exporter based on OTEL_EXPORTER_OTLP_PROTOCOL and skips
// setup if OTEL_EXPORTER_OTLP_ENDPOINT is not set.
func InitOtelLogging(ctx context.Context) (func(context.Context) error, error) {
	var le sdklog.Exporter
	var err error

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))

	switch {
    case endpoint == "":
        slog.Info("OTEL_EXPORTER_OTLP_ENDPOINT not set")
        slog.Info("Using NoOp exporters")
        le = nil
    case strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))) == "grpc":
        slog.Info("Using OTLP gRPC exporters")
        le, err = otlploggrpc.New(ctx)
    default:
        slog.Info("Using OTLP HTTP exporters")
        le, err = otlploghttp.New(ctx)
	}
	if err != nil {
			return nil, err
	}

	var sdkProvider *sdklog.LoggerProvider
	var provider otellog.LoggerProvider 
	
	// default to noop providers
	provider = noopsdklog.NewLoggerProvider()

	if le != nil {
		sdkProvider = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(le)),
		)
		provider = sdkProvider
	}
	global.SetLoggerProvider(provider)

	shutdown := func(ctx context.Context) error {
			if sdkProvider == nil {
					return nil
			}
			return sdkProvider.Shutdown(ctx)
    }

	return shutdown, nil
}

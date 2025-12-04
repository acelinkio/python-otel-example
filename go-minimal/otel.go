package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/log/global"
)

// InitOtelLogging creates the OTLP log exporter and returns a shutdown function.
// It chooses gRPC or HTTP exporter based on OTEL_EXPORTER_OTLP_PROTOCOL and skips
// setup if OTEL_EXPORTER_OTLP_ENDPOINT is not set.
func InitOtelLogging(ctx context.Context) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		slog.Info("OTEL_EXPORTER_OTLP_ENDPOINT not set, skipping OTLP logs setup")
		// return a no-op shutdown to avoid nil callers
		return func(context.Context) error { return nil }, nil
	}

	var exp sdklog.Exporter
	var err error

	if strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))) == "grpc" {
		slog.Info("using otlp grpc log exporter", "endpoint", endpoint)
		exp, err = otlploggrpc.New(ctx)
	} else {
		slog.Info("using otlp http log exporter", "endpoint", endpoint)
		exp, err = otlploghttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	)

	global.SetLoggerProvider(provider)

	shutdown := func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	}

	return shutdown, nil
}

package main

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/log/global"
)

// InitOtelLogging creates the OTLP log exporter and returns a shutdown function.
// It stores the provider in a package-level variable so InitLogger doesn't need it.
func InitOtelLogging(ctx context.Context) (func(context.Context) error, error) {
	exp, err := otlploggrpc.New(ctx)
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

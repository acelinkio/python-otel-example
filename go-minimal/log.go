package main

import (
    "context"
    "os"

    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
    sdklog "go.opentelemetry.io/otel/sdk/log"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"

    "go.opentelemetry.io/contrib/bridges/otelzap"
)

// InitLogger sets up the OTLP exporter + zap logger. It returns the logger,
// a shutdown function that must be called before exit, and an error.
func InitLogger(ctx context.Context) (*zap.Logger, func(context.Context) error, error) {
    exp, err := otlploggrpc.New(ctx)
    if err != nil {
        return nil, nil, err
    }

    provider := sdklog.NewLoggerProvider(
        sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
    )

    core := zapcore.NewTee(
        zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(os.Stdout), zapcore.InfoLevel),
        otelzap.NewCore("my/pkg/name", otelzap.WithLoggerProvider(provider)),
    )

    logger := zap.New(core)

    shutdown := func(ctx context.Context) error {
        return provider.Shutdown(ctx)
    }

    return logger, shutdown, nil
}
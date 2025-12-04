package main

import (
	"context"
	"log/slog"
)

func main() {
	ctx := context.Background()

	shutdownOtel, err := InitOtelLogging(ctx)
	if err != nil {
		slog.Error("otel init", "err", err)
	}
	defer func() { _ = shutdownOtel(ctx) }()

	logger, _, err := InitLogger(ctx)
	if err != nil {
		slog.Error("logger init", "err", err)
	}
	defer logger.Sync()

	slog.Info("info: dog barks")
	slog.Warn("warning: don't 123")
	slog.Error("error: hey0")
}

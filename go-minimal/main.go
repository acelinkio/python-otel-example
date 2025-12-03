package main

import (
	"context"
	"log"
)

func main() {
	ctx := context.Background()

	provider, shutdownOtel, err := InitOtelLogging(ctx)
	if err != nil {
		log.Fatalf("otel init: %v", err)
	}
	defer shutdownOtel(ctx)

	logger, _, err := InitLogger(ctx, provider)
	if err != nil {
		log.Fatalf("logger init: %v", err)
	}
	defer logger.Sync()

	logger.Info("started")
	logger.Error("O HELL YEAH")
}

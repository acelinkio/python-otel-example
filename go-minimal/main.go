package main

import (
	"context"
	"log"
	"time"
)

func main() {
	ctx := context.Background()

	logger, shutdown, err := InitLogger(ctx)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}

	// ensure provider is shutdown (flush) before exit
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("logger shutdown error: %v", err)
		}
	}()

	logger.Info("something really cool 2 me")
	logger.Error("an example error123123")
}

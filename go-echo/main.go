package main

import (
	"context"
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"log/slog"
	"net/http"
)

func main() {
	ctx := context.Background()

	logger, _, err := SetupLogger(ctx)
	if err != nil {
		slog.Error("logger init", "err", err)
	}
	defer logger.Sync()

	cleanup, err := SetupOtel(ctx)
	if err != nil {
		slog.Error("otel init", "err", err)
	}
	defer cleanup(ctx)

	slog.Info("info: dog barks")
	slog.Warn("warning: don't 123")
	slog.Error("error: hey0123")

	e := echo.New()
	e.Logger.SetOutput(logger)
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.GET("/", hello)
	// Start server
	if err := e.Start(":8025"); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("failed to start server", "error", err)
	}
}

func hello(c echo.Context) error {
	slog.Info("WHY HELLO THERE")
	return c.String(http.StatusOK, "Hello, World!")
}
package main

import (
	"context"
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
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

	// build ignore set from env or fall back to defaults
	var ignore map[string]struct{}
	if val, ok := os.LookupEnv("LOG_IGNORE_WEBPATHS"); !ok {
		// env not provided -> use defaults
		ignore = map[string]struct{}{
			"/health":      {},
			"/favicon.ico": {},
			"/ready":       {},
		}
	} else {

		ignore = make(map[string]struct{})
		val = strings.TrimSpace(val)
		if val != "" {
			for _, p := range strings.Split(val, ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				ignore[p] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(ignore))
	for p := range ignore {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		slog.Info("web_request.ignore_paths", "paths", "none")
	} else {
		slog.Info("web_request.ignore_paths", "paths", strings.Join(paths, ","))
	}

	// using example from https://echo.labstack.com/docs/middleware/logger#examples
	// full configs https://github.com/labstack/echo/blob/master/middleware/request_logger.go
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		// declare a small set of paths to ignore
		Skipper: func(c echo.Context) bool {
			p := c.Request().URL.Path
			_, skip := ignore[p]
			return skip
		},
		LogStatus:    true,
		LogURI:       true,
		LogError:     true,
		LogHost:      true,
		LogMethod:    true,
		LogUserAgent: true,
		HandleError:  true, // forwards error to the global error handler, so it can decide appropriate status code
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				logger.LogAttrs(context.Background(), slog.LevelInfo, "web_request",
					slog.String("method", v.Method),
					slog.Int("status", v.Status),
					slog.String("host", v.Host),
					slog.String("uri", v.URI),
					slog.String("agent", v.UserAgent),
				)
			} else {
				logger.LogAttrs(context.Background(), slog.LevelError, "web_request_error",
					slog.String("method", v.Method),
					slog.Int("status", v.Status),
					slog.String("host", v.Host),
					slog.String("uri", v.URI),
					slog.String("agent", v.UserAgent),
					slog.String("err", v.Error.Error()),
				)
			}
			return nil
		},
	}))

	e.Use(middleware.Recover())

	e.GET("/", hello)
	e.GET("/health", health)
	// Start server
	if err := e.Start(":8025"); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("failed to start server", "error", err)
	}
}

func hello(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

func health(c echo.Context) error {
	return c.String(http.StatusOK, "I AM HEALTHY")
}
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
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

func SetupEcho(ctx context.Context, logger *SlogAdapter) error {
	e := echo.New()
	e.HideBanner = true
	e.Logger.SetOutput(logger)

	// build ignore list from env or fall back to defaults
	var ignore []string
	if val, ok := os.LookupEnv("LOG_IGNORE_WEBPATHS"); !ok {
		slog.Info("env LOG_IGNORE_WEBPATHS not set, using defaults")
		ignore = []string{
			"/health",
			"/favicon.ico",
			"/ready",
		}
	} else {
		val = strings.TrimSpace(val)
		if val != "" {
			for _, p := range strings.Split(val, ",") {
				if p = strings.TrimSpace(p); p != "" {
					ignore = append(ignore, p)
				}
			}
		}
	}
	sort.Strings(ignore)
	stringignore := strings.Join(ignore, ",")
	slog.Info("web_request.log_ignore_paths", "paths", stringignore)

	contains := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}
	// using example from https://echo.labstack.com/docs/middleware/logger#examples
	// full configs https://github.com/labstack/echo/blob/master/middleware/request_logger.go
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		// declare a small set of paths to ignore
		Skipper: func(c echo.Context) bool {
			p := c.Request().URL.Path
			return contains(ignore, p)
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
				logger.LogAttrs(ctx, slog.LevelInfo, "web_request",
					slog.String("method", v.Method),
					slog.Int("status", v.Status),
					slog.String("host", v.Host),
					slog.String("uri", v.URI),
					slog.String("agent", v.UserAgent),
				)
			} else {
				logger.LogAttrs(ctx, slog.LevelError, "web_request_error",
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
	e.Use(otelecho.Middleware("http.server/echo", otelecho.WithSkipper(func(c echo.Context) bool {
		return c.Path() == "/auth/health" || c.Path() == "/auth/ready"
	})))	

	e.GET("/", hello)
	e.GET("/health", health)
	if err := e.Start(":8025"); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func hello(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

func health(c echo.Context) error {
	return c.String(http.StatusOK, "I AM HEALTHY")
}

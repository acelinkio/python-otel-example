package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
)

// SlogAdapter embeds slog.Logger and adds a Sync method so callers that expect
// a zap-like logger can still call Sync().
type SlogAdapter struct {
	*slog.Logger
}

func (a *SlogAdapter) Sync() error { return nil }

type tee struct {
	a, b slog.Handler
	minA slog.Level
	minB slog.Level
}

// a = stdout
// b = otel
// minA = minimum level for stdout
// minB = minimum level for otel (from OTEL_LOG_LEVEL)
func NewTee(a, b slog.Handler, minA, minB slog.Level) slog.Handler {
	return &tee{a: a, b: b, minA: minA, minB: minB}
}

func (t *tee) Enabled(ctx context.Context, level slog.Level) bool {
	if (t.minA == 0 || level >= t.minA) && t.a.Enabled(ctx, level) {
		return true
	}
	if (t.minB == 0 || level >= t.minB) && t.b.Enabled(ctx, level) {
		return true
	}
	return false
}

func (t *tee) Handle(ctx context.Context, r slog.Record) error {
	if t.minA == 0 || r.Level >= t.minA {
		if err := t.a.Handle(ctx, r); err != nil {
			return err
		}
	}
	if t.minB == 0 || r.Level >= t.minB {
		return t.b.Handle(ctx, r)
	}
	return nil
}

func (t *tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewTee(t.a.WithAttrs(attrs), t.b.WithAttrs(attrs), t.minA, t.minB)
}

func (t *tee) WithGroup(name string) slog.Handler {
	return NewTee(t.a.WithGroup(name), t.b.WithGroup(name), t.minA, t.minB)
}

func InitLogger(ctx context.Context) (*SlogAdapter, func(context.Context) error, error) {
	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})
	otelHandler := otelslog.NewHandler("mylog", otelslog.WithLoggerProvider(global.GetLoggerProvider()))

	minStdout := parseLogLevel(os.Getenv("STDOUT_LOG_LEVEL"))
	minOtel := parseLogLevel(os.Getenv("OTEL_LOG_LEVEL"))

	handler := NewTee(stdout, otelHandler, minStdout, minOtel)

	logger := slog.New(handler)
	adapter := &SlogAdapter{logger}
	shutdown := func(ctx context.Context) error { return nil }

	slog.SetDefault(adapter.Logger)
	return adapter, shutdown, nil
}

func parseLogLevel(v string) slog.Level {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return slog.LevelInfo
	}
	m := map[string]slog.Level{
		"TRACE":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"WARN":    slog.LevelWarn,
		"WARNING": slog.LevelWarn,
		"ERROR":   slog.LevelError,
	}
	if lv, ok := m[v]; ok {
		return lv
	}
	return slog.LevelInfo
}

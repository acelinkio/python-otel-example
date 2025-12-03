package main

import (
    "context"
    "log/slog"
    "os"

    otellog "go.opentelemetry.io/otel/log"
    otelslog "go.opentelemetry.io/contrib/bridges/otelslog"
)

// SlogAdapter embeds slog.Logger and adds a Sync method so callers that expect
// a zap-like logger can still call Sync().
type SlogAdapter struct {
    *slog.Logger
}

func (a *SlogAdapter) Sync() error { return nil }

// multiHandler -> small tee that forwards to two handlers with fewer lines.
type tee struct {
    a, b slog.Handler
}

func NewTee(a, b slog.Handler) slog.Handler { return &tee{a: a, b: b} }

func (t *tee) Enabled(ctx context.Context, level slog.Level) bool {
    return t.a.Enabled(ctx, level) || t.b.Enabled(ctx, level)
}

func (t *tee) Handle(ctx context.Context, r slog.Record) error {
    _ = t.a.Handle(ctx, r)
    return t.b.Handle(ctx, r)
}

func (t *tee) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &tee{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t *tee) WithGroup(name string) slog.Handler {
    return &tee{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
}

type levelFilter struct {
    min slog.Level
    h   slog.Handler
}

func (f *levelFilter) Enabled(ctx context.Context, level slog.Level) bool {
    if level < f.min {
        return false
    }
    return f.h.Enabled(ctx, level)
}

func (f *levelFilter) Handle(ctx context.Context, r slog.Record) error {
    if r.Level < f.min {
        return nil
    }
    return f.h.Handle(ctx, r)
}

func (f *levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &levelFilter{min: f.min, h: f.h.WithAttrs(attrs)}
}

func (f *levelFilter) WithGroup(name string) slog.Handler {
    return &levelFilter{min: f.min, h: f.h.WithGroup(name)}
}

// InitLogger sets up a slog logger. It keeps the same signature (accepting an
// otel API LoggerProvider) so the provider can be passed through; currently
// provider isn't used by this simple adapter but can be wired into a custom
// Handler if you want OpenTelemetry forwarding.
func InitLogger(ctx context.Context, provider otellog.LoggerProvider) (*SlogAdapter, func(context.Context) error, error) {
    // stdout JSON handler
    stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})

    // filter stdout to Info+ (change slog.LevelInfo to desired minimum)
    stdoutFiltered := &levelFilter{min: slog.LevelInfo, h: stdout}

    // OTEL bridge handler (for shipping to collector)
    otelHandler := otelslog.NewHandler("my/pkg/name", otelslog.WithLoggerProvider(provider))

    // combine both so logs go to stdout AND OTLP exporter
    handler := NewTee(stdoutFiltered, otelHandler)
    
    logger := slog.New(handler)
    adapter := &SlogAdapter{logger}

    shutdown := func(ctx context.Context) error { return nil }
    return adapter, shutdown, nil
}
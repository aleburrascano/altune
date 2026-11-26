package logging

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

type SetupOption func(*setupConfig)

type setupConfig struct {
	now func() time.Time
}

func WithClock(now func() time.Time) SetupOption {
	if now == nil {
		panic("logging: WithClock requires a clock")
	}
	return func(c *setupConfig) { c.now = now }
}

func Setup(logLevel string, opts ...SetupOption) *RingBuffer {
	return install(logLevel, jsonHandler, opts)
}

func SetupDevelopment(logLevel string, opts ...SetupOption) *RingBuffer {
	return install(logLevel, prettyHandlerFor, opts)
}

func jsonHandler(level slog.Level) slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})
}

func prettyHandlerFor(level slog.Level) slog.Handler {
	return &prettyHandler{
		level:     level,
		w:         os.Stdout,
		addSource: true,
	}
}

func install(logLevel string, newBase func(slog.Level) slog.Handler, opts []SetupOption) *RingBuffer {
	cfg := setupConfig{now: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}
	base := newBase(parseLevel(logLevel))

	ring := NewRingBuffer(logRingCapacity, cfg.now)
	slog.SetDefault(slog.New(newCorrelationHandler(newRingHandler(base, ring))))
	return ring
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

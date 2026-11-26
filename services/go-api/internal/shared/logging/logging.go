package logging

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

func Setup(logLevel string) *RingBuffer {
	return install(logLevel, jsonHandler, time.Now)
}

func SetupWithClock(logLevel string, now func() time.Time) *RingBuffer {
	if now == nil {
		panic("logging: SetupWithClock requires a clock")
	}
	return install(logLevel, jsonHandler, now)
}

func SetupDevelopment(logLevel string) *RingBuffer {
	return install(logLevel, prettyHandlerFor, time.Now)
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

func install(logLevel string, newBase func(slog.Level) slog.Handler, now func() time.Time) *RingBuffer {
	base := newBase(parseLevel(logLevel))

	ring := MustNewRingBuffer(logRingCapacity, now)
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

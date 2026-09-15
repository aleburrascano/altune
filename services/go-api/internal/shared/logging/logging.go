package logging

import (
	"log/slog"
	"os"
	"strings"
)

func Setup(logLevel string, development bool) *RingBuffer {
	level := parseLevel(logLevel)

	var base slog.Handler
	if development {
		base = &prettyHandler{
			level:     level,
			w:         os.Stdout,
			addSource: true,
		}
	} else {
		base = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		})
	}

	ring := NewRingBuffer(logRingCapacity)
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

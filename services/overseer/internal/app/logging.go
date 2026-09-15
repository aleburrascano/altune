package app

import (
	"log/slog"
	"os"
	"strings"
)

// SetupLogging installs a process-wide slog logger, matching go-api's split of
// JSON in production and a readable text handler in development.
func SetupLogging(level string, development bool) {
	opts := &slog.HandlerOptions{Level: parseLevel(level), AddSource: true}
	var h slog.Handler
	if development {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
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

package app

import (
	"log/slog"
	"os"
	"strings"
)

func SetupTextLogging(level string) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, handlerOptions(level))))
}

func SetupJSONLogging(level string) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, handlerOptions(level))))
}

func handlerOptions(level string) *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: parseLevel(level), AddSource: true}
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

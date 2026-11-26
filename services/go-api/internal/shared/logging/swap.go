package logging

import "log/slog"

func Swap(l *slog.Logger) (restore func()) {
	prev := slog.Default()
	slog.SetDefault(l)
	return func() { slog.SetDefault(prev) }
}

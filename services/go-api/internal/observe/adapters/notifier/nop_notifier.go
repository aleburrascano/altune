package notifier

import (
	"altune/go-api/internal/observe/domain"
	"context"
	"log/slog"
)

type NopNotifier struct{}

func (NopNotifier) Silent() bool { return true }

func (NopNotifier) Notify(ctx context.Context, a domain.Alert) error {
	slog.ErrorContext(ctx, "alert.signal", "title", a.Title, "message", a.Message)
	return nil
}

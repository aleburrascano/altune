package alert

import (
	"context"
	"log/slog"
)

type NopNotifier struct{}

func (NopNotifier) Notify(ctx context.Context, a Alert) error {
	slog.ErrorContext(ctx, "alert.signal", "title", a.Title, "message", a.Message)
	return nil
}

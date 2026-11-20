package ports

import (
	"altune/go-api/internal/observe/internal/core/domain"
	"context"
)

type AlertNotifier interface {
	Notify(ctx context.Context, a domain.Alert) error
}

type SilentNotifier interface {
	Silent() bool
}

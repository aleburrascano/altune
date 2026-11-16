package ports

import (
	"altune/go-api/internal/observe/domain"
	"context"
)

type AlertNotifier interface {
	Notify(ctx context.Context, a domain.Alert) error
}

type SilentNotifier interface {
	Silent() bool
}

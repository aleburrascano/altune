package ports

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"context"
)

type AlertNotifier interface {
	Notify(ctx context.Context, a domain.Alert) error
	Silent() bool
}

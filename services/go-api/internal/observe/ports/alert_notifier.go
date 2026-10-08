package ports

import (
	"context"

	"altune/go-api/internal/observe/domain"
)

type AlertNotifier interface {
	Notify(ctx context.Context, a domain.Alert) error
}

// SilentNotifier is implemented by notifiers that deliver nothing to a person.
type SilentNotifier interface {
	Silent() bool
}

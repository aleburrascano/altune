package ports

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"context"
	"errors"
	"time"
)

var ErrClientEventsUnavailable = errors.New("client event store unavailable")

type ClientEventStore interface {
	Append(ctx context.Context, event domain.ClientEvent) error
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
	Prune(ctx context.Context, now time.Time) (int64, error)
}

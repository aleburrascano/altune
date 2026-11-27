package ports

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"context"
	"errors"
)

var ErrClientEventsUnavailable = errors.New("client event store unavailable")

type ClientEventStore interface {
	Append(ctx context.Context, event domain.ClientEvent) error
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}

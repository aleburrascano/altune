package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"time"
)

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

type ActivityFeed interface {
	EmitActivity(eventType string)
}

type QueryCount struct {
	QueryNorm string
	Count     int
}

type BehavioralSignal struct {
	ResultSignature string
	Score           float64
}

type BehavioralSignalStore interface {
	SatisfactionSignals(ctx context.Context, since time.Time) ([]BehavioralSignal, error)
}

type EventQuery interface {
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

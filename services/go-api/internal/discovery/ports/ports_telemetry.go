package ports

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

type SearchHistoryRepository interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId shared.UserId, n int) error
	ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

// EventStore appends telemetry interaction events (§8 of the discovery rebuild
// blueprint). Append is best-effort from the caller's perspective — emission is
// async and a failure must never surface to the user request.
type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

// QueryCount is a query_norm with how many times it occurred in a window.
type QueryCount struct {
	QueryNorm string
	Count     int
}

// EventQuery reads aggregated telemetry for the offline coverage signals. These
// are analytics reads over discovery's own tables — never the request path.
type EventQuery interface {
	// ZeroResultQueries ranks search queries that returned nothing in the window.
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	// NonZeroNoClickQueries ranks queries that returned results but drew no click
	// for that query_norm in the window (a weak coverage hint).
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

type FetchSuccessStore interface {
	Record(ctx context.Context, provider domain.ProviderName, success bool) error
	GetRate(ctx context.Context, provider domain.ProviderName) (float64, error)
}

type VocabularyStore interface {
	Add(ctx context.Context, entry domain.VocabularyEntry) error
	BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

type ChartProvider interface {
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}

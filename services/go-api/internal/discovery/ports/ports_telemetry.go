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
	DeleteAllForUser(ctx context.Context, userId shared.UserId) error
}

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
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

type EventConsumer interface {
	Name() string
	Signals(ctx context.Context, since time.Time) ([]BehavioralSignal, error)
}

type BehavioralLabel struct {
	QueryNorm       string
	ResultSignature string
	Title           string
	Subtitle        string
	Polarity        int
}

type BehavioralLabelStore interface {
	BehavioralLabels(ctx context.Context, since time.Time) ([]BehavioralLabel, error)
}

type EventQuery interface {
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

type MetricPoint struct {
	AsOf  time.Time
	Value float64
}

type MetricsRollupStore interface {
	RollupDay(ctx context.Context, day time.Time) error
	MetricsHistory(ctx context.Context, metric string, days int) ([]MetricPoint, error)
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

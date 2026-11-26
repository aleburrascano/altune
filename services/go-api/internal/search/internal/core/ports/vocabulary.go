package ports

import (
	"altune/go-api/internal/search/domain"
	"context"
)

type SpellingHintsReader interface {
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.SpellingHintsEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.SpellingHintsEntry, error)
}

type SpellingHintsWriter interface {
	Add(ctx context.Context, entry domain.SpellingHintsEntry) error
	BulkAdd(ctx context.Context, entries []domain.SpellingHintsEntry) error
	Trim(ctx context.Context, maxEntries int) error
}

type SpellingHintsStore interface {
	SpellingHintsReader
	SpellingHintsWriter
}

type ChartProvider interface {
	Name() domain.ProviderName
	FetchCharts(ctx context.Context, limit int) ([]domain.SpellingHintsEntry, error)
}

package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type ChartProvider interface {
	Name() domain.ProviderName
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}

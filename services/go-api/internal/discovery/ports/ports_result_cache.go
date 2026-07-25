package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

type ResultCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}

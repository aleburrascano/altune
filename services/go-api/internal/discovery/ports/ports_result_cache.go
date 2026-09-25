package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type ResultCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}

type HeldSlateCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}

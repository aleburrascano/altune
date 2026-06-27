package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
)

// ResultCache is an app-wide (not per-user) short-TTL cache of a query's final
// ranked results. Discovery results are catalog-derived, not user-specific, so a
// shared key is correct — and it is the point: the same query returns the same
// list for everyone within the window, smoothing the provider-drop-out and
// cache-warmth variance that otherwise makes identical queries diverge run-to-run.
// Best-effort: a cache failure must never fail a search, so the methods swallow
// errors. Redis-backed in production, no-op when Redis is absent.
type ResultCache interface {
	Get(ctx context.Context, key string) ([]domain.SearchResult, bool)
	Set(ctx context.Context, key string, results []domain.SearchResult)
}

package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sort"
	"strings"
)

// searchResultCache is the result-caching collaborator: the app-wide, short-TTL
// cache of a query's final ranked list, keyed by normalized query + kinds. It
// wraps the injected ResultCache port so the cache-key definition and the
// consult/bypass decision live in one unit instead of inline in Execute. A nil
// port or an empty normalized query (symbol-only queries share no key) bypasses
// the cache entirely, never touching the port.
type searchResultCache struct {
	cache ports.ResultCache
}

func newSearchResultCache(cache ports.ResultCache) *searchResultCache {
	return &searchResultCache{cache: cache}
}

func (c *searchResultCache) enabled(queryNorm string) bool {
	return c.cache != nil && queryNorm != ""
}

func (c *searchResultCache) get(
	ctx context.Context,
	queryNorm string,
	kinds map[domain.ResultKind]bool,
) ([]domain.SearchResult, bool) {
	if !c.enabled(queryNorm) {
		return nil, false
	}
	return c.cache.Get(ctx, c.key(queryNorm, kinds))
}

func (c *searchResultCache) set(
	ctx context.Context,
	queryNorm string,
	kinds map[domain.ResultKind]bool,
	ranked []domain.SearchResult,
) {
	if !c.enabled(queryNorm) {
		return
	}
	c.cache.Set(ctx, c.key(queryNorm, kinds), ranked)
}

func (c *searchResultCache) key(queryNorm string, kinds map[domain.ResultKind]bool) string {
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	return queryNorm + "|" + strings.Join(ks, ",")
}

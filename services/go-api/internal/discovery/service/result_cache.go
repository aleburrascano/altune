package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// searchResultCache is the result-caching collaborator: the app-wide, short-TTL
// cache of a query's final ranked list, keyed by normalized query + kinds, plus
// the slate one search holds for its own later pages, keyed by search id. It
// wraps the injected ResultCache port so the cache-key definition and the
// consult/bypass decision live in one unit instead of inline in Execute. A nil
// port or an empty normalized query (symbol-only queries share no query key)
// bypasses the query-keyed cache entirely, never touching the port; a held
// slate shares no key with another search, so it is kept for those queries too.
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

// heldSlate returns the ranked slate a search kept for its later pages, when
// the caller names a search whose slate is still there.
func (c *searchResultCache) heldSlate(
	ctx context.Context,
	searchId uuid.UUID,
	queryNorm string,
	kinds map[domain.ResultKind]bool,
) ([]domain.SearchResult, bool) {
	if c.cache == nil || searchId == uuid.Nil {
		return nil, false
	}
	return c.cache.Get(ctx, c.heldSlateKey(searchId, queryNorm, kinds))
}

// holdSlate keeps one search's whole ranked list so its later pages are cut
// from the same ranking. Unlike the query-keyed entry it is kept whatever the
// slate's provenance — a partial or corrected slate is precisely the one a
// second fan-out would rank differently — since only the search that produced
// it can read it back. It holds the ranking as it stands before the per-user
// favorites lift, which every page re-applies, exactly as the query-keyed
// entry does.
func (c *searchResultCache) holdSlate(
	ctx context.Context,
	searchId uuid.UUID,
	queryNorm string,
	kinds map[domain.ResultKind]bool,
	ranked []domain.SearchResult,
) {
	if c.cache == nil || searchId == uuid.Nil || len(ranked) == 0 {
		return
	}
	c.cache.Set(ctx, c.heldSlateKey(searchId, queryNorm, kinds), ranked)
}

// heldSlateKey binds a held slate to the query it answers, so a search id
// replayed against another query or kind set misses instead of paging one
// search's results into another's.
func (c *searchResultCache) heldSlateKey(
	searchId uuid.UUID,
	queryNorm string,
	kinds map[domain.ResultKind]bool,
) string {
	return "slate|" + searchId.String() + "|" + c.key(queryNorm, kinds)
}

func (c *searchResultCache) key(queryNorm string, kinds map[domain.ResultKind]bool) string {
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	return queryNorm + "|" + strings.Join(ks, ",")
}

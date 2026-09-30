package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"

	"github.com/google/uuid"
)

type searchResultCache struct {
	cache      ports.ResultCache
	heldSlates ports.HeldSlateCache
}

func newSearchResultCache(cache ports.ResultCache, heldSlates ports.HeldSlateCache) *searchResultCache {
	return &searchResultCache{cache: cache, heldSlates: heldSlates}
}

func (c *searchResultCache) enabled(queryNorm string) bool {
	return c.cache != nil && queryNorm != ""
}

func (c *searchResultCache) get(
	ctx context.Context,
	queryNorm string,
	kinds domain.ResultKindSet,
) ([]domain.SearchResult, bool) {
	if !c.enabled(queryNorm) {
		return nil, false
	}
	return c.cache.Get(ctx, c.key(queryNorm, kinds))
}

func (c *searchResultCache) set(
	ctx context.Context,
	queryNorm string,
	kinds domain.ResultKindSet,
	ranked []domain.SearchResult,
) {
	if !c.enabled(queryNorm) {
		return
	}
	c.cache.Set(ctx, c.key(queryNorm, kinds), ranked)
}

func (c *searchResultCache) heldSlate(
	ctx context.Context,
	searchId uuid.UUID,
	queryNorm string,
	kinds domain.ResultKindSet,
) ([]domain.SearchResult, bool) {
	if c.heldSlates == nil || searchId == uuid.Nil {
		return nil, false
	}
	return c.heldSlates.Get(ctx, c.heldSlateKey(searchId, queryNorm, kinds))
}

func (c *searchResultCache) holdSlate(
	ctx context.Context,
	searchId uuid.UUID,
	queryNorm string,
	kinds domain.ResultKindSet,
	ranked []domain.SearchResult,
) {
	if c.heldSlates == nil || searchId == uuid.Nil || len(ranked) == 0 {
		return
	}
	c.heldSlates.Set(ctx, c.heldSlateKey(searchId, queryNorm, kinds), ranked)
}

func (c *searchResultCache) heldSlateKey(
	searchId uuid.UUID,
	queryNorm string,
	kinds domain.ResultKindSet,
) string {
	return "slate|" + searchId.String() + "|" + c.key(queryNorm, kinds)
}

func (c *searchResultCache) key(queryNorm string, kinds domain.ResultKindSet) string {
	return queryNorm + "|" + kinds.Key()
}

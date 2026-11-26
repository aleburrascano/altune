package cache

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.MBIDIndex = (*RedisEnrichmentCache)(nil)

const (
	enrichmentPositiveTTL = 14 * 24 * time.Hour
	enrichmentNegativeTTL = 24 * time.Hour
)

type RedisEnrichmentCache struct {
	redisJSON
}

func NewRedisEnrichmentCache(client *goredis.Client, opts ...Option) *RedisEnrichmentCache {
	return &RedisEnrichmentCache{newRedisJSON(client, opts)}
}

func (c *RedisEnrichmentCache) Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MusicBrainzEnrichment, bool, error) {
	if c.disabled() {
		return domain.EmptyEnrichment(), false, nil
	}
	e, ok := getJSON[domain.MusicBrainzEnrichment](ctx, c.redisJSON, enrichmentKey(kind, mbid))
	if !ok {
		return domain.EmptyEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisEnrichmentCache) Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MusicBrainzEnrichment) error {
	if c.disabled() {
		return nil
	}
	return c.setJSON(ctx, enrichmentKey(kind, mbid), e, enrichmentPositiveTTL)
}

func (c *RedisEnrichmentCache) GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error) {
	if c.disabled() {
		return false, nil
	}
	return c.getNegative(ctx, enrichmentNegKey(kind, nameKey))
}

func (c *RedisEnrichmentCache) SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error {
	if c.disabled() {
		return nil
	}
	if err := c.client.Set(ctx, enrichmentNegKey(kind, nameKey), redisNegSentinel, enrichmentNegativeTTL).Err(); err != nil {
		return fmt.Errorf("remember negative enrichment %s: %w", nameKey, err)
	}
	return nil
}

func (c *RedisEnrichmentCache) LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool) {
	if c.disabled() || nameKey == "" {
		return "", false
	}
	cached, err := c.client.Get(ctx, mbidIndexKey(kind, nameKey)).Result()
	if err != nil || cached == "" {
		return "", false
	}
	return cached, true
}

func (c *RedisEnrichmentCache) RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error {
	if c.disabled() || nameKey == "" || mbid == "" {
		return nil
	}
	if err := c.client.Set(ctx, mbidIndexKey(kind, nameKey), mbid, enrichmentPositiveTTL).Err(); err != nil {
		return fmt.Errorf("remember mbid of %s: %w", nameKey, err)
	}
	return nil
}

func mbidIndexKey(kind domain.ResultKind, nameKey string) string {
	return hashKey("discovery:mbid:v2:"+kind.String()+":", nameKey)
}

func enrichmentKey(kind domain.ResultKind, mbid string) string {
	return fmt.Sprintf("discovery:mbenrich:v1:%s:%s", kind.String(), mbid)
}

func enrichmentNegKey(kind domain.ResultKind, nameKey string) string {
	return hashKey("discovery:mbenrich:neg:v2:"+kind.String()+":", nameKey)
}

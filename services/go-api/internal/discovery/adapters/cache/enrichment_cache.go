package cache

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var (
	_ ports.IdentityBridge = (*RedisEnrichmentCache)(nil)
	_ ports.MBIDIndex      = (*RedisEnrichmentCache)(nil)
)

const (
	enrichmentPositiveTTL = 14 * 24 * time.Hour
	enrichmentNegativeTTL = 24 * time.Hour
)

type RedisEnrichmentCache struct {
	redisJSON
}

func NewRedisEnrichmentCache(client *goredis.Client) *RedisEnrichmentCache {
	return &RedisEnrichmentCache{redisJSON{client: client}}
}

func (c *RedisEnrichmentCache) Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error) {
	if c.disabled() {
		return domain.EmptyEnrichment(), false, nil
	}
	e, ok := getJSON[domain.MBEnrichment](ctx, c.redisJSON, enrichmentKey(kind, mbid))
	if !ok {
		return domain.EmptyEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisEnrichmentCache) Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error {
	if c.disabled() {
		return nil
	}
	return c.setJSON(ctx, enrichmentKey(kind, mbid), e, enrichmentPositiveTTL)
}

func (c *RedisEnrichmentCache) GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error) {
	if c.disabled() {
		return false, nil
	}
	_, err := c.client.Get(ctx, enrichmentNegKey(kind, nameKey)).Result()
	return err == nil, nil
}

func (c *RedisEnrichmentCache) SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error {
	if c.disabled() {
		return nil
	}
	return c.client.Set(ctx, enrichmentNegKey(kind, nameKey), redisNegSentinel, enrichmentNegativeTTL).Err()
}

func (c *RedisEnrichmentCache) ExternalIDs(ctx context.Context, kind domain.ResultKind, mbid string) (map[string]string, bool) {
	if c.disabled() || mbid == "" {
		return nil, false
	}
	e, found, _ := c.Get(ctx, kind, mbid)
	if !found || len(e.ExternalIDs) == 0 {
		return nil, false
	}
	return e.ExternalIDs, true
}

func (c *RedisEnrichmentCache) LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool) {
	if c.disabled() || nameKey == "" {
		return "", false
	}
	val, err := c.client.Get(ctx, mbidIndexKey(kind, nameKey)).Result()
	if err != nil || val == "" {
		return "", false
	}
	return val, true
}

func (c *RedisEnrichmentCache) RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error {
	if c.disabled() || nameKey == "" || mbid == "" {
		return nil
	}
	return c.client.Set(ctx, mbidIndexKey(kind, nameKey), mbid, enrichmentPositiveTTL).Err()
}

func mbidIndexKey(kind domain.ResultKind, nameKey string) string {
	return hashKey("discovery:mbid:v1:"+kind.String()+":", nameKey)
}

func enrichmentKey(kind domain.ResultKind, mbid string) string {
	return fmt.Sprintf("discovery:mbenrich:v1:%s:%s", kind.String(), mbid)
}

func enrichmentNegKey(kind domain.ResultKind, nameKey string) string {
	return hashKey("discovery:mbenrich:neg:v1:"+kind.String()+":", nameKey)
}

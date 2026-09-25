package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// resultCachePrefix is versioned by the cached SearchResult JSON shape. v2:
// record_type and resolution_tier moved from Extras to typed fields, so v1
// entries must not be decoded by new code (nor v2 entries by old instances
// mid-deploy); the 45s TTL makes the cold window negligible.
const (
	resultCachePrefix = "discovery:results:v2:"
	resultCacheTTL    = 45 * time.Second

	heldSlatePrefix = "discovery:heldslate:v2:"
	heldSlateTTL    = 30 * time.Minute
)

type RedisResultCache struct {
	base RedisNameKeyedCache[[]domain.SearchResult]
}

func newRedisResultCache(client *goredis.Client, prefix string, ttl time.Duration) *RedisResultCache {
	return &RedisResultCache{
		base: RedisNameKeyedCache[[]domain.SearchResult]{
			redisJSON: redisJSON{client: client},
			posPrefix: prefix,
			posTTL:    ttl,
			empty:     func() []domain.SearchResult { return nil },
		},
	}
}

func NewRedisResultCache(client *goredis.Client) *RedisResultCache {
	return newRedisResultCache(client, resultCachePrefix, resultCacheTTL)
}

func NewRedisHeldSlateCache(client *goredis.Client) *RedisResultCache {
	return newRedisResultCache(client, heldSlatePrefix, heldSlateTTL)
}

func (c *RedisResultCache) Get(ctx context.Context, key string) ([]domain.SearchResult, bool) {
	results, hit, _ := c.base.Get(ctx, key)
	return results, hit
}

func (c *RedisResultCache) Set(ctx context.Context, key string, results []domain.SearchResult) {
	_ = c.base.Set(ctx, key, results)
}

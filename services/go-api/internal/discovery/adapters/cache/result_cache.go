package cache

import (
	"context"
	"encoding/json"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

// resultCacheTTL is the determinism window: an identical query returns the
// identical ranked list app-wide for this long, smoothing provider drop-out and
// cache-warmth variance run-to-run. Kept short so a newly-acquired track or a
// shipped ranking change surfaces within the minute.
const resultCacheTTL = 45 * time.Second

// RedisResultCache is the app-wide short-TTL cache of a query's final ranked
// results. No-op when Redis is absent.
type RedisResultCache struct {
	client *goredis.Client
}

func NewRedisResultCache(client *goredis.Client) *RedisResultCache {
	return &RedisResultCache{client: client}
}

func (c *RedisResultCache) Get(ctx context.Context, key string) ([]domain.SearchResult, bool) {
	if c.client == nil {
		return nil, false
	}
	val, err := c.client.Get(ctx, resultCacheKey(key)).Result()
	if err != nil {
		return nil, false
	}
	var results []domain.SearchResult
	if err := json.Unmarshal([]byte(val), &results); err != nil {
		// Corrupt/legacy value: miss so it recomputes and overwrites.
		return nil, false
	}
	return results, true
}

func (c *RedisResultCache) Set(ctx context.Context, key string, results []domain.SearchResult) {
	if c.client == nil {
		return
	}
	payload, err := json.Marshal(results)
	if err != nil {
		return
	}
	// Best-effort: a cache write failure must never fail the search.
	_ = c.client.Set(ctx, resultCacheKey(key), payload, resultCacheTTL).Err()
}

// resultCacheKey namespaces and hashes the composite query key. Bump the version
// on any change to what the cached value contains.
func resultCacheKey(key string) string {
	return hashKey("discovery:results:v1:", key)
}

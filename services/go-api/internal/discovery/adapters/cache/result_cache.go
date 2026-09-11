package cache

import (
	"context"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const (
	resultCachePrefix = "discovery:results:v1:"
	resultCacheTTL    = 45 * time.Second
)

type RedisResultCache struct {
	base RedisNameKeyedCache[[]domain.SearchResult]
}

func NewRedisResultCache(client *goredis.Client) *RedisResultCache {
	return &RedisResultCache{
		base: RedisNameKeyedCache[[]domain.SearchResult]{
			redisJSON: redisJSON{client: client},
			posPrefix: resultCachePrefix,
			posTTL:    resultCacheTTL,
			empty:     func() []domain.SearchResult { return nil },
		},
	}
}

func (c *RedisResultCache) Get(ctx context.Context, key string) ([]domain.SearchResult, bool) {
	results, hit, _ := c.base.Get(ctx, key)
	return results, hit
}

func (c *RedisResultCache) Set(ctx context.Context, key string, results []domain.SearchResult) {
	_ = c.base.Set(ctx, key, results)
}

func resultCacheKey(key string) string {
	return hashKey(resultCachePrefix, key)
}

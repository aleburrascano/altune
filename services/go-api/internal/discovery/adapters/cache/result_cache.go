package cache

import (
	"context"
	"encoding/json"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const resultCacheTTL = 45 * time.Second

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
	_ = c.client.Set(ctx, resultCacheKey(key), payload, resultCacheTTL).Err()
}

func resultCacheKey(key string) string {
	return hashKey("discovery:results:v1:", key)
}

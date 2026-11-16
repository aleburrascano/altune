package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	resultCachePrefix = "discovery:results:v2:"
	resultCacheTTL    = 45 * time.Second

	heldSlatePrefix = "discovery:heldslate:v2:"
	heldSlateTTL    = 30 * time.Minute
)

type RedisResultCache struct {
	base RedisNameKeyedCache[[]domain.SearchResult]
}

func newRedisResultCache(
	client *goredis.Client,
	prefix string,
	ttl time.Duration,
	opts []Option,
) *RedisResultCache {
	return &RedisResultCache{
		base: RedisNameKeyedCache[[]domain.SearchResult]{
			redisJSON: newRedisJSON(client, opts),
			posPrefix: prefix,
			posTTL:    ttl,
			empty:     func() []domain.SearchResult { return nil },
		},
	}
}

func NewRedisResultCache(client *goredis.Client, opts ...Option) *RedisResultCache {
	return newRedisResultCache(client, resultCachePrefix, resultCacheTTL, opts)
}

func NewRedisHeldSlateCache(client *goredis.Client, opts ...Option) *RedisResultCache {
	return newRedisResultCache(client, heldSlatePrefix, heldSlateTTL, opts)
}

func (c *RedisResultCache) Get(ctx context.Context, key string) ([]domain.SearchResult, bool) {
	results, hit, _ := c.base.Get(ctx, key)
	return results, hit
}

func (c *RedisResultCache) Set(ctx context.Context, key string, results []domain.SearchResult) {
	_ = c.base.Set(ctx, key, results)
}

const providerResultPrefix = "discovery:provider:v1:"

type ProviderCacheTTLs struct {
	Default  time.Duration
	Provider map[domain.ProviderName]time.Duration
}

func (t ProviderCacheTTLs) of(provider domain.ProviderName) time.Duration {
	if ttl, ok := t.Provider[provider]; ok {
		return ttl
	}
	return t.Default
}

type RedisProviderResultCache struct {
	redisJSON
	ttls ProviderCacheTTLs
}

func NewRedisProviderResultCache(client *goredis.Client, ttls ProviderCacheTTLs, opts ...Option) *RedisProviderResultCache {
	return &RedisProviderResultCache{redisJSON: newRedisJSON(client, opts), ttls: ttls}
}

func (c *RedisProviderResultCache) Get(ctx context.Context, provider domain.ProviderName, key string) ([]domain.SearchResult, bool) {
	if c.disabled() {
		return nil, false
	}
	return getJSON[[]domain.SearchResult](ctx, c.redisJSON, c.redisKey(provider, key))
}

func (c *RedisProviderResultCache) Set(ctx context.Context, provider domain.ProviderName, key string, results []domain.SearchResult) {
	if c.disabled() {
		return
	}
	if results == nil {
		results = []domain.SearchResult{}
	}
	_ = c.setJSON(ctx, c.redisKey(provider, key), results, c.ttls.of(provider))
}

func (c *RedisProviderResultCache) redisKey(provider domain.ProviderName, key string) string {
	return hashKey(providerResultPrefix, provider.String()+"|"+key)
}

package cache

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const (
	artworkPositiveTTL = 14 * 24 * time.Hour
	artworkNegativeTTL = 24 * time.Hour
)

type RedisArtworkCache struct {
	client *goredis.Client
}

func NewRedisArtworkCache(client *goredis.Client) *RedisArtworkCache {
	return &RedisArtworkCache{client: client}
}

func (c *RedisArtworkCache) Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, bool, error) {
	if c.client == nil {
		return "", false, nil
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return "", false, nil
	}

	return val, true, nil
}

func (c *RedisArtworkCache) Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string) error {
	if c.client == nil {
		return nil
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)
	ttl := artworkPositiveTTL
	if url == "" {
		ttl = artworkNegativeTTL
	}

	return c.client.Set(ctx, key, url, ttl).Err()
}

func artworkCacheKey(kind domain.ResultKind, title, subtitle, mbid string) string {
	// v2: identity-aware resolution (artwork keyed to the proven identity, not a
	// name guess). The bump abandons v1 entries — which cached wrong same-name
	// faces (the "Che" problem) — so the fix takes effect immediately; stale keys
	// expire by TTL. Bump this on any change to how artwork is resolved.
	input := fmt.Sprintf("%s|%s|%s", title, subtitle, mbid)
	return hashKey("discovery:artwork:v2:"+kind.String()+":", input)
}

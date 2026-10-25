package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.UnreleasedCache = (*RedisUnreleasedCache)(nil)

type RedisUnreleasedCache struct {
	redisJSON
}

func NewRedisUnreleasedCache(client *goredis.Client, opts ...Option) *RedisUnreleasedCache {
	return &RedisUnreleasedCache{newRedisJSON(client, opts)}
}

func (c *RedisUnreleasedCache) Get(ctx context.Context, provider domain.ProviderName, externalID, nameKey string) ([]domain.UnreleasedSong, bool) {
	if c.disabled() {
		return nil, false
	}
	return getJSON[[]domain.UnreleasedSong](ctx, c.redisJSON, unreleasedKey(provider, externalID, nameKey))
}

func (c *RedisUnreleasedCache) Set(ctx context.Context, provider domain.ProviderName, externalID, nameKey string, songs []domain.UnreleasedSong, ttl time.Duration) error {
	if c.disabled() {
		return nil
	}
	return c.setJSON(ctx, unreleasedKey(provider, externalID, nameKey), songs, ttl)
}

const unreleasedKeyVersion = "v8"

func unreleasedKey(provider domain.ProviderName, externalID, nameKey string) string {
	return fmt.Sprintf("unreleased:songs:%s:%s:%s:%s", unreleasedKeyVersion, provider.Key(), externalID, nameKey)
}

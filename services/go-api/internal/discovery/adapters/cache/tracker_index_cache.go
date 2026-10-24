package cache

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.TrackerIndexCache = (*RedisTrackerIndexCache)(nil)

const trackerIndexKey = "trackers:hub:v1"

type RedisTrackerIndexCache struct {
	redisJSON
}

func NewRedisTrackerIndexCache(client *goredis.Client, opts ...Option) *RedisTrackerIndexCache {
	return &RedisTrackerIndexCache{newRedisJSON(client, opts)}
}

func (c *RedisTrackerIndexCache) Get(ctx context.Context) (map[string]string, bool) {
	if c.disabled() {
		return nil, false
	}
	return getJSON[map[string]string](ctx, c.redisJSON, trackerIndexKey)
}

func (c *RedisTrackerIndexCache) Set(ctx context.Context, index map[string]string, ttl time.Duration) error {
	if c.disabled() {
		return nil
	}
	return c.setJSON(ctx, trackerIndexKey, index, ttl)
}

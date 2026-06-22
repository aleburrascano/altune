package cache

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	discogsPositiveTTL  = 30 * 24 * time.Hour
	discogsNegativeTTL  = 24 * time.Hour
	discogsNoneSentinel = "__none__"
)

type CachedDiscogsResolver struct {
	client *goredis.Client
}

func NewCachedDiscogsResolver(client *goredis.Client) *CachedDiscogsResolver {
	return &CachedDiscogsResolver{client: client}
}

func (c *CachedDiscogsResolver) Get(ctx context.Context, artistName string) (int, bool) {
	if c.client == nil {
		return 0, false
	}
	key := discogsCacheKey(artistName)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	if val == discogsNoneSentinel {
		return 0, true
	}
	id, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	return id, true
}

func (c *CachedDiscogsResolver) Set(ctx context.Context, artistName string, discogsID int) {
	if c.client == nil {
		return
	}
	key := discogsCacheKey(artistName)
	var err error
	if discogsID > 0 {
		err = c.client.Set(ctx, key, strconv.Itoa(discogsID), discogsPositiveTTL).Err()
	} else {
		err = c.client.Set(ctx, key, discogsNoneSentinel, discogsNegativeTTL).Err()
	}
	if err != nil {
		slog.WarnContext(ctx, "discogs_cache.set_failed", "key", key, "error", err)
	}
}

func discogsCacheKey(artistName string) string {
	return hashKey("discovery:discogs:v1:", artistName)
}

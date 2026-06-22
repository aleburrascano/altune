package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.LastFmEnrichmentCache = (*RedisLastFmEnrichmentCache)(nil)

const (
	lastfmEnrichmentPositiveTTL = 30 * 24 * time.Hour // Last.fm data drifts slowly
	lastfmEnrichmentNegativeTTL = 24 * time.Hour
	lastfmEnrichmentNegValue    = "1"
)

// RedisLastFmEnrichmentCache is a read-through cache of LastFmEnrichment keyed
// by a normalized (kind, artist, title) name key. Positive entries store the
// whole value object; the negative path records that a name resolved to
// nothing. A nil client is a no-op (Get miss / Set no-op), so the service runs
// uncached without Redis.
type RedisLastFmEnrichmentCache struct {
	client *goredis.Client
}

func NewRedisLastFmEnrichmentCache(client *goredis.Client) *RedisLastFmEnrichmentCache {
	return &RedisLastFmEnrichmentCache{client: client}
}

func (c *RedisLastFmEnrichmentCache) Get(ctx context.Context, nameKey string) (domain.LastFmEnrichment, bool, error) {
	if c.client == nil {
		return domain.EmptyLastFmEnrichment(), false, nil
	}
	val, err := c.client.Get(ctx, lastfmEnrichmentKey(nameKey)).Result()
	if err != nil {
		return domain.EmptyLastFmEnrichment(), false, nil
	}
	var e domain.LastFmEnrichment
	if err := json.Unmarshal([]byte(val), &e); err != nil {
		return domain.EmptyLastFmEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisLastFmEnrichmentCache) Set(ctx context.Context, nameKey string, e domain.LastFmEnrichment) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, lastfmEnrichmentKey(nameKey), blob, lastfmEnrichmentPositiveTTL).Err()
}

func (c *RedisLastFmEnrichmentCache) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, lastfmEnrichmentNegKey(nameKey)).Result()
	return err == nil, nil
}

func (c *RedisLastFmEnrichmentCache) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, lastfmEnrichmentNegKey(nameKey), lastfmEnrichmentNegValue, lastfmEnrichmentNegativeTTL).Err()
}

func lastfmEnrichmentKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:lfmenrich:v1:%x", h[:16])
}

func lastfmEnrichmentNegKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:lfmenrich:neg:v1:%x", h[:16])
}

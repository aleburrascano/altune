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

var _ ports.DiscogsEnrichmentCache = (*RedisDiscogsEnrichmentCache)(nil)

const (
	discogsEnrichmentPositiveTTL = 30 * 24 * time.Hour // Discogs data is near-static
	discogsEnrichmentNegativeTTL = 24 * time.Hour
	discogsEnrichmentNegValue    = "1"
)

// RedisDiscogsEnrichmentCache is a read-through cache of DiscogsEnrichment keyed
// by a normalized (artist, album) name key. Positive entries store the whole
// value object; the negative path records that a name resolved to nothing. A nil
// client is a no-op (Get miss / Set no-op), so the service runs uncached without
// Redis.
type RedisDiscogsEnrichmentCache struct {
	client *goredis.Client
}

func NewRedisDiscogsEnrichmentCache(client *goredis.Client) *RedisDiscogsEnrichmentCache {
	return &RedisDiscogsEnrichmentCache{client: client}
}

func (c *RedisDiscogsEnrichmentCache) Get(ctx context.Context, nameKey string) (domain.DiscogsEnrichment, bool, error) {
	if c.client == nil {
		return domain.EmptyDiscogsEnrichment(), false, nil
	}
	val, err := c.client.Get(ctx, discogsEnrichmentKey(nameKey)).Result()
	if err != nil {
		return domain.EmptyDiscogsEnrichment(), false, nil
	}
	var e domain.DiscogsEnrichment
	if err := json.Unmarshal([]byte(val), &e); err != nil {
		return domain.EmptyDiscogsEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisDiscogsEnrichmentCache) Set(ctx context.Context, nameKey string, e domain.DiscogsEnrichment) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, discogsEnrichmentKey(nameKey), blob, discogsEnrichmentPositiveTTL).Err()
}

func (c *RedisDiscogsEnrichmentCache) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, discogsEnrichmentNegKey(nameKey)).Result()
	return err == nil, nil
}

func (c *RedisDiscogsEnrichmentCache) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, discogsEnrichmentNegKey(nameKey), discogsEnrichmentNegValue, discogsEnrichmentNegativeTTL).Err()
}

func discogsEnrichmentKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dgenrich:v1:%x", h[:16])
}

func discogsEnrichmentNegKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dgenrich:neg:v1:%x", h[:16])
}

var _ ports.DiscogsArtistEnrichmentCache = (*RedisDiscogsArtistEnrichmentCache)(nil)

// RedisDiscogsArtistEnrichmentCache is the artist-scoped sibling of
// RedisDiscogsEnrichmentCache, keyed by a normalized artist name. A nil client
// is a no-op, so the service runs uncached without Redis.
type RedisDiscogsArtistEnrichmentCache struct {
	client *goredis.Client
}

func NewRedisDiscogsArtistEnrichmentCache(client *goredis.Client) *RedisDiscogsArtistEnrichmentCache {
	return &RedisDiscogsArtistEnrichmentCache{client: client}
}

func (c *RedisDiscogsArtistEnrichmentCache) Get(ctx context.Context, nameKey string) (domain.DiscogsArtistEnrichment, bool, error) {
	if c.client == nil {
		return domain.EmptyDiscogsArtistEnrichment(), false, nil
	}
	val, err := c.client.Get(ctx, discogsArtistEnrichmentKey(nameKey)).Result()
	if err != nil {
		return domain.EmptyDiscogsArtistEnrichment(), false, nil
	}
	var e domain.DiscogsArtistEnrichment
	if err := json.Unmarshal([]byte(val), &e); err != nil {
		return domain.EmptyDiscogsArtistEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisDiscogsArtistEnrichmentCache) Set(ctx context.Context, nameKey string, e domain.DiscogsArtistEnrichment) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, discogsArtistEnrichmentKey(nameKey), blob, discogsEnrichmentPositiveTTL).Err()
}

func (c *RedisDiscogsArtistEnrichmentCache) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, discogsArtistEnrichmentNegKey(nameKey)).Result()
	return err == nil, nil
}

func (c *RedisDiscogsArtistEnrichmentCache) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, discogsArtistEnrichmentNegKey(nameKey), discogsEnrichmentNegValue, discogsEnrichmentNegativeTTL).Err()
}

func discogsArtistEnrichmentKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dgartist:v1:%x", h[:16])
}

func discogsArtistEnrichmentNegKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dgartist:neg:v1:%x", h[:16])
}

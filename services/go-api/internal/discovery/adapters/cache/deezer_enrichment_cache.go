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

var _ ports.DeezerEnrichmentCache = (*RedisDeezerEnrichmentCache)(nil)

const (
	deezerEnrichmentPositiveTTL = 30 * 24 * time.Hour // Deezer detail data is near-static
	deezerEnrichmentNegativeTTL = 24 * time.Hour
	deezerEnrichmentNegValue    = "1"
)

// RedisDeezerEnrichmentCache is a read-through cache of DeezerEnrichment keyed by
// a normalized (kind, artist, title) name key. Positive entries store the whole
// value object; the negative path records that a name resolved to nothing. A nil
// client is a no-op (Get miss / Set no-op), so the service runs uncached without
// Redis.
type RedisDeezerEnrichmentCache struct {
	client *goredis.Client
}

func NewRedisDeezerEnrichmentCache(client *goredis.Client) *RedisDeezerEnrichmentCache {
	return &RedisDeezerEnrichmentCache{client: client}
}

func (c *RedisDeezerEnrichmentCache) Get(ctx context.Context, nameKey string) (domain.DeezerEnrichment, bool, error) {
	if c.client == nil {
		return domain.EmptyDeezerEnrichment(), false, nil
	}
	val, err := c.client.Get(ctx, deezerEnrichmentKey(nameKey)).Result()
	if err != nil {
		return domain.EmptyDeezerEnrichment(), false, nil
	}
	var e domain.DeezerEnrichment
	if err := json.Unmarshal([]byte(val), &e); err != nil {
		return domain.EmptyDeezerEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisDeezerEnrichmentCache) Set(ctx context.Context, nameKey string, e domain.DeezerEnrichment) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, deezerEnrichmentKey(nameKey), blob, deezerEnrichmentPositiveTTL).Err()
}

func (c *RedisDeezerEnrichmentCache) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, deezerEnrichmentNegKey(nameKey)).Result()
	return err == nil, nil
}

func (c *RedisDeezerEnrichmentCache) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, deezerEnrichmentNegKey(nameKey), deezerEnrichmentNegValue, deezerEnrichmentNegativeTTL).Err()
}

func deezerEnrichmentKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dzenrich:v1:%x", h[:16])
}

func deezerEnrichmentNegKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dzenrich:neg:v1:%x", h[:16])
}

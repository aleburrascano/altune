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

var _ ports.LyricsCache = (*RedisDeezerLyricsCache)(nil)

const (
	deezerLyricsPositiveTTL = 90 * 24 * time.Hour // lyrics are static — cache long
	deezerLyricsNegativeTTL = 24 * time.Hour      // availability is region/catalog dependent
	deezerLyricsNegValue    = "1"
)

// RedisDeezerLyricsCache is a read-through cache of DeezerLyrics keyed by a
// normalized (artist, title) name key. Positive entries store the whole value
// object; the negative path records that a name produced no lyrics. A nil client
// is a no-op (Get miss / Set no-op), so the service runs uncached without Redis.
type RedisDeezerLyricsCache struct {
	client *goredis.Client
}

func NewRedisDeezerLyricsCache(client *goredis.Client) *RedisDeezerLyricsCache {
	return &RedisDeezerLyricsCache{client: client}
}

func (c *RedisDeezerLyricsCache) Get(ctx context.Context, nameKey string) (domain.DeezerLyrics, bool, error) {
	if c.client == nil {
		return domain.EmptyDeezerLyrics(), false, nil
	}
	val, err := c.client.Get(ctx, deezerLyricsKey(nameKey)).Result()
	if err != nil {
		return domain.EmptyDeezerLyrics(), false, nil
	}
	var l domain.DeezerLyrics
	if err := json.Unmarshal([]byte(val), &l); err != nil {
		return domain.EmptyDeezerLyrics(), false, nil
	}
	return l, true, nil
}

func (c *RedisDeezerLyricsCache) Set(ctx context.Context, nameKey string, l domain.DeezerLyrics) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, deezerLyricsKey(nameKey), blob, deezerLyricsPositiveTTL).Err()
}

func (c *RedisDeezerLyricsCache) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, deezerLyricsNegKey(nameKey)).Result()
	return err == nil, nil
}

func (c *RedisDeezerLyricsCache) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, deezerLyricsNegKey(nameKey), deezerLyricsNegValue, deezerLyricsNegativeTTL).Err()
}

func deezerLyricsKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dzlyrics:v1:%x", h[:16])
}

func deezerLyricsNegKey(nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:dzlyrics:neg:v1:%x", h[:16])
}

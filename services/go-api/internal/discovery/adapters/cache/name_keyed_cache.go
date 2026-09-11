package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var (
	_ ports.DeezerEnrichmentCache = (*RedisNameKeyedCache[domain.DeezerEnrichment])(nil)
	_ ports.LastFmEnrichmentCache = (*RedisNameKeyedCache[domain.LastFmEnrichment])(nil)
	_ ports.LyricsCache           = (*RedisNameKeyedCache[domain.DeezerLyrics])(nil)
)

const (
	nameKeyedPositiveTTL = 30 * 24 * time.Hour
	nameKeyedNegativeTTL = 24 * time.Hour
	lyricsPositiveTTL    = 90 * 24 * time.Hour
)

type RedisNameKeyedCache[T any] struct {
	redisJSON
	posPrefix string
	negPrefix string
	posTTL    time.Duration
	negTTL    time.Duration
	empty     func() T
}

func (c *RedisNameKeyedCache[T]) Get(ctx context.Context, nameKey string) (T, bool, error) {
	if c.disabled() {
		return c.empty(), false, nil
	}
	v, ok := getJSON[T](ctx, c.redisJSON, hashKey(c.posPrefix, nameKey))
	if !ok {
		return c.empty(), false, nil
	}
	return v, true, nil
}

func (c *RedisNameKeyedCache[T]) Set(ctx context.Context, nameKey string, v T) error {
	if c.disabled() {
		return nil
	}
	return c.setJSON(ctx, hashKey(c.posPrefix, nameKey), v, c.posTTL)
}

func (c *RedisNameKeyedCache[T]) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.disabled() {
		return false, nil
	}
	_, err := c.client.Get(ctx, hashKey(c.negPrefix, nameKey)).Result()
	return err == nil, nil
}

func (c *RedisNameKeyedCache[T]) SetNegative(ctx context.Context, nameKey string) error {
	if c.disabled() {
		return nil
	}
	return c.client.Set(ctx, hashKey(c.negPrefix, nameKey), redisNegSentinel, c.negTTL).Err()
}

func hashKey(prefix, nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("%s%x", prefix, h[:16])
}

func NewRedisNameKeyedCache[T any](client *goredis.Client, posPrefix, negPrefix string, posTTL, negTTL time.Duration, empty func() T) *RedisNameKeyedCache[T] {
	return &RedisNameKeyedCache[T]{
		redisJSON: redisJSON{client: client},
		posPrefix: posPrefix,
		negPrefix: negPrefix,
		posTTL:    posTTL,
		negTTL:    negTTL,
		empty:     empty,
	}
}

func NewRedisDeezerEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.DeezerEnrichment] {
	return &RedisNameKeyedCache[domain.DeezerEnrichment]{
		redisJSON: redisJSON{client: client},
		posPrefix: "discovery:dzenrich:v1:",
		negPrefix: "discovery:dzenrich:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerEnrichment,
	}
}

func NewRedisLastFmEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.LastFmEnrichment] {
	return &RedisNameKeyedCache[domain.LastFmEnrichment]{
		redisJSON: redisJSON{client: client},
		posPrefix: "discovery:lfmenrich:v1:",
		negPrefix: "discovery:lfmenrich:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyLastFmEnrichment,
	}
}

func NewRedisDeezerLyricsCache(client *goredis.Client) *RedisNameKeyedCache[domain.DeezerLyrics] {
	return &RedisNameKeyedCache[domain.DeezerLyrics]{
		redisJSON: redisJSON{client: client},
		posPrefix: "discovery:dzlyrics:v1:",
		negPrefix: "discovery:dzlyrics:neg:v1:",
		posTTL:    lyricsPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerLyrics,
	}
}

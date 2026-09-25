package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

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
	return c.getNegative(ctx, hashKey(c.negPrefix, nameKey))
}

func (c *RedisNameKeyedCache[T]) SetNegative(ctx context.Context, nameKey string) error {
	if c.disabled() {
		return nil
	}
	return c.setRaw(ctx, hashKey(c.negPrefix, nameKey), redisNegSentinel, c.negTTL)
}

func hashKey(prefix, nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("%s%x", prefix, h[:16])
}

func NewRedisNameKeyedCache[T any](client *goredis.Client, posPrefix, negPrefix string, posTTL, negTTL time.Duration, empty func() T, opts ...Option) *RedisNameKeyedCache[T] {
	return &RedisNameKeyedCache[T]{
		redisJSON: newRedisJSON(client, opts),
		posPrefix: posPrefix,
		negPrefix: negPrefix,
		posTTL:    posTTL,
		negTTL:    negTTL,
		empty:     empty,
	}
}

func NewRedisDeezerEnrichmentCache(client *goredis.Client, opts ...Option) *RedisNameKeyedCache[domain.DeezerEnrichment] {
	return &RedisNameKeyedCache[domain.DeezerEnrichment]{
		redisJSON: newRedisJSON(client, opts),
		posPrefix: "discovery:dzenrich:v2:",
		negPrefix: "discovery:dzenrich:neg:v2:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerEnrichment,
	}
}

func NewRedisLastFmEnrichmentCache(client *goredis.Client, opts ...Option) *RedisNameKeyedCache[domain.LastFmEnrichment] {
	return &RedisNameKeyedCache[domain.LastFmEnrichment]{
		redisJSON: newRedisJSON(client, opts),
		posPrefix: "discovery:lfmenrich:v2:",
		negPrefix: "discovery:lfmenrich:neg:v2:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyLastFmEnrichment,
	}
}

func NewRedisDeezerLyricsCache(client *goredis.Client, opts ...Option) *RedisNameKeyedCache[domain.DeezerLyrics] {
	return &RedisNameKeyedCache[domain.DeezerLyrics]{
		redisJSON: newRedisJSON(client, opts),
		posPrefix: "discovery:dzlyrics:v2:",
		negPrefix: "discovery:dzlyrics:neg:v2:",
		posTTL:    lyricsPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerLyrics,
	}
}

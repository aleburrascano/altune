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

// Compile-time proof that the one generic adapter satisfies every name-keyed
// enrichment cache port (the per-provider interfaces are aliases of
// ports.NameKeyedCache[T]).
var (
	_ ports.DeezerEnrichmentCache        = (*RedisNameKeyedCache[domain.DeezerEnrichment])(nil)
	_ ports.LastFmEnrichmentCache        = (*RedisNameKeyedCache[domain.LastFmEnrichment])(nil)
	_ ports.DiscogsEnrichmentCache       = (*RedisNameKeyedCache[domain.DiscogsEnrichment])(nil)
	_ ports.DiscogsArtistEnrichmentCache = (*RedisNameKeyedCache[domain.DiscogsArtistEnrichment])(nil)
	_ ports.LyricsCache                  = (*RedisNameKeyedCache[domain.DeezerLyrics])(nil)
)

// negValue is the sentinel stored under a negative key; only its existence is
// checked (Get err == nil), never its content.
const negValue = "1"

const (
	nameKeyedPositiveTTL = 30 * 24 * time.Hour // detail data is near-static
	nameKeyedNegativeTTL = 24 * time.Hour      // a name's resolvability can change
	lyricsPositiveTTL    = 90 * 24 * time.Hour // lyrics are static — cache long
)

// RedisNameKeyedCache is the one read-through Redis adapter behind every
// name-keyed detail-enrichment cache (Deezer, Last.fm, Discogs album/artist,
// lyrics). Each provider differs only by value type T, key prefixes, and TTLs,
// supplied by its constructor below; the positive path stores the whole value
// object, the negative path a marker that a name resolved to nothing. A nil
// client is a no-op (Get miss / Set no-op), so the services run uncached without
// Redis.
type RedisNameKeyedCache[T any] struct {
	client    *goredis.Client
	posPrefix string
	negPrefix string
	posTTL    time.Duration
	negTTL    time.Duration
	empty     func() T
}

func (c *RedisNameKeyedCache[T]) Get(ctx context.Context, nameKey string) (T, bool, error) {
	if c.client == nil {
		return c.empty(), false, nil
	}
	val, err := c.client.Get(ctx, hashKey(c.posPrefix, nameKey)).Result()
	if err != nil {
		return c.empty(), false, nil
	}
	var v T
	if err := json.Unmarshal([]byte(val), &v); err != nil {
		return c.empty(), false, nil
	}
	return v, true, nil
}

func (c *RedisNameKeyedCache[T]) Set(ctx context.Context, nameKey string, v T) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, hashKey(c.posPrefix, nameKey), blob, c.posTTL).Err()
}

func (c *RedisNameKeyedCache[T]) GetNegative(ctx context.Context, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, hashKey(c.negPrefix, nameKey)).Result()
	return err == nil, nil
}

func (c *RedisNameKeyedCache[T]) SetNegative(ctx context.Context, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, hashKey(c.negPrefix, nameKey), negValue, c.negTTL).Err()
}

// hashKey hashes the variable-length, user-derived name key under a fixed prefix
// so the stored Redis key is bounded and opaque. Format is unchanged from the
// per-provider adapters this generic replaced (prefix + first 16 bytes of the
// SHA-256, hex), so existing cached keys remain valid.
func hashKey(prefix, nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("%s%x", prefix, h[:16])
}

// NewRedisDeezerEnrichmentCache caches DeezerEnrichment by normalized
// (kind, artist, title) name key.
func NewRedisDeezerEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.DeezerEnrichment] {
	return &RedisNameKeyedCache[domain.DeezerEnrichment]{
		client:    client,
		posPrefix: "discovery:dzenrich:v1:",
		negPrefix: "discovery:dzenrich:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerEnrichment,
	}
}

// NewRedisLastFmEnrichmentCache caches LastFmEnrichment by normalized
// (kind, artist, title) name key.
func NewRedisLastFmEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.LastFmEnrichment] {
	return &RedisNameKeyedCache[domain.LastFmEnrichment]{
		client:    client,
		posPrefix: "discovery:lfmenrich:v1:",
		negPrefix: "discovery:lfmenrich:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyLastFmEnrichment,
	}
}

// NewRedisDiscogsEnrichmentCache caches DiscogsEnrichment by normalized
// (artist, album) name key.
func NewRedisDiscogsEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.DiscogsEnrichment] {
	return &RedisNameKeyedCache[domain.DiscogsEnrichment]{
		client:    client,
		posPrefix: "discovery:dgenrich:v1:",
		negPrefix: "discovery:dgenrich:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDiscogsEnrichment,
	}
}

// NewRedisDiscogsArtistEnrichmentCache caches DiscogsArtistEnrichment by
// normalized artist name.
func NewRedisDiscogsArtistEnrichmentCache(client *goredis.Client) *RedisNameKeyedCache[domain.DiscogsArtistEnrichment] {
	return &RedisNameKeyedCache[domain.DiscogsArtistEnrichment]{
		client:    client,
		posPrefix: "discovery:dgartist:v1:",
		negPrefix: "discovery:dgartist:neg:v1:",
		posTTL:    nameKeyedPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDiscogsArtistEnrichment,
	}
}

// NewRedisDeezerLyricsCache caches DeezerLyrics by normalized (artist, title)
// name key. Positive entries get a long TTL (lyrics are static); negative
// entries a short one (availability is region/catalog dependent).
func NewRedisDeezerLyricsCache(client *goredis.Client) *RedisNameKeyedCache[domain.DeezerLyrics] {
	return &RedisNameKeyedCache[domain.DeezerLyrics]{
		client:    client,
		posPrefix: "discovery:dzlyrics:v1:",
		negPrefix: "discovery:dzlyrics:neg:v1:",
		posTTL:    lyricsPositiveTTL,
		negTTL:    nameKeyedNegativeTTL,
		empty:     domain.EmptyDeezerLyrics,
	}
}

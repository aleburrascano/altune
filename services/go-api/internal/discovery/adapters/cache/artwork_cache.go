package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.ArtworkCache = (*RedisArtworkCache)(nil)

type artworkEntry struct {
	URL        string `json:"u"`
	Source     string `json:"s"`
	Confidence int    `json:"c,omitempty"`
}

const artworkSetAttempts = 5

const (
	artworkPositiveTTL    = 14 * 24 * time.Hour
	artworkProvisionalTTL = 48 * time.Hour

	artworkNegativeTTLTrack  = 6 * time.Hour
	artworkNegativeTTLAlbum  = 12 * time.Hour
	artworkNegativeTTLArtist = 24 * time.Hour
)

func negativeTTL(kind domain.ResultKind) time.Duration {
	switch kind {
	case domain.ResultKindTrack:
		return artworkNegativeTTLTrack
	case domain.ResultKindAlbum:
		return artworkNegativeTTLAlbum
	default:
		return artworkNegativeTTLArtist
	}
}

type RedisArtworkCache struct {
	redisJSON
}

func NewRedisArtworkCache(client *goredis.Client, opts ...Option) *RedisArtworkCache {
	return &RedisArtworkCache{newRedisJSON(client, opts)}
}

func (c *RedisArtworkCache) Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, domain.ProviderKey, bool, error) {
	if c.disabled() {
		return "", "", false, nil
	}

	entry, ok := c.read(ctx, artworkCacheKey(kind, title, subtitle, mbid))
	if !ok {
		return "", "", false, nil
	}
	return entry.URL, domain.ProviderKey(entry.Source), true, nil
}

func (c *RedisArtworkCache) Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string, source domain.ProviderKey, confidence ports.ArtworkConfidence) error {
	if c.disabled() {
		return nil
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)
	entry := artworkEntry{URL: url, Source: source.String(), Confidence: int(confidence)}
	blob, err := json.Marshal(entry)
	if err != nil {
		c.signal.failure(ctx, kindOf(key), opEncode, err)
		return err
	}
	ttl := artworkTTL(kind, url, confidence)

	for range artworkSetAttempts {
		err = c.client.Watch(ctx, func(tx *goredis.Tx) error {
			return c.setIfNotDowngrade(ctx, tx, key, blob, int(confidence), ttl)
		}, key)
		if !errors.Is(err, goredis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("artwork set: key kept changing across %d attempts: %w", artworkSetAttempts, goredis.TxFailedErr)
}

func (c *RedisArtworkCache) setIfNotDowngrade(ctx context.Context, tx *goredis.Tx, key string, blob []byte, confidence int, ttl time.Duration) error {
	raw, err := tx.Get(ctx, key).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return err
	}
	var existing artworkEntry
	if err == nil && json.Unmarshal([]byte(raw), &existing) == nil && existing.URL != "" && confidence < existing.Confidence {
		return nil
	}
	_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		return pipe.Set(ctx, key, blob, ttl).Err()
	})
	if err != nil && !errors.Is(err, goredis.TxFailedErr) {
		c.signal.failure(ctx, kindOf(key), opSet, err)
	}
	return err
}

func (c *RedisArtworkCache) read(ctx context.Context, key string) (artworkEntry, bool) {
	return getJSON[artworkEntry](ctx, c.redisJSON, key)
}

func artworkTTL(kind domain.ResultKind, url string, confidence ports.ArtworkConfidence) time.Duration {
	if url == "" {
		return negativeTTL(kind)
	}
	if confidence >= ports.ArtworkConfidenceIdentity {
		return artworkPositiveTTL
	}
	return artworkProvisionalTTL
}

func artworkCacheKey(kind domain.ResultKind, title, subtitle, mbid string) string {
	input := fmt.Sprintf("%s|%s|%s", title, subtitle, mbid)
	return hashKey("discovery:artwork:v3:"+kind.String()+":", input)
}

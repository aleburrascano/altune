package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.ArtworkCache = (*RedisArtworkCache)(nil)

type artworkEntry struct {
	URL        string `json:"u"`
	Source     string `json:"s"`
	Confidence int    `json:"c,omitempty"`
}

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
	client *goredis.Client
}

func NewRedisArtworkCache(client *goredis.Client) *RedisArtworkCache {
	return &RedisArtworkCache{client: client}
}

func (c *RedisArtworkCache) Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, string, bool, error) {
	if c.client == nil {
		return "", "", false, nil
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return "", "", false, nil
	}

	var entry artworkEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		return "", "", false, nil
	}
	return entry.URL, entry.Source, true, nil
}

func (c *RedisArtworkCache) Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url, source string, confidence ports.ArtworkConfidence) error {
	if c.client == nil {
		return nil
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)

	if existing, ok := c.read(ctx, key); ok && existing.URL != "" && int(confidence) < existing.Confidence {
		return nil
	}

	payload, err := json.Marshal(artworkEntry{URL: url, Source: source, Confidence: int(confidence)})
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, payload, artworkTTL(kind, url, confidence)).Err()
}

func (c *RedisArtworkCache) read(ctx context.Context, key string) (artworkEntry, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return artworkEntry{}, false
	}
	var entry artworkEntry
	if json.Unmarshal([]byte(val), &entry) != nil {
		return artworkEntry{}, false
	}
	return entry, true
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

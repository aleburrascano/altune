package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

// artworkEntry is the cached value: the resolved URL plus the source that
// supplied it ("fanart", "discogs", …), so a cache hit still reports who
// resolved the artwork for per-provider coverage visibility. A negative entry
// has an empty URL.
type artworkEntry struct {
	URL    string `json:"u"`
	Source string `json:"s"`
}

const (
	artworkPositiveTTL = 14 * 24 * time.Hour

	// Negative (no-artwork) TTLs are per-kind: a missing image is cached so the
	// 8-provider chain isn't re-run every search, but the wait before newly-added
	// artwork appears scales with how fast that kind churns. Tracks churn most
	// (singles/deep cuts get art late), artists least (stable, rarely gain a
	// photo), so tracks re-check soonest.
	artworkNegativeTTLTrack  = 6 * time.Hour
	artworkNegativeTTLAlbum  = 12 * time.Hour
	artworkNegativeTTLArtist = 24 * time.Hour
)

// negativeTTL returns the no-artwork cache TTL for a kind (artist is the
// conservative default for any unknown kind).
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
		// Corrupt/legacy value: treat as a miss so it re-resolves and overwrites.
		return "", "", false, nil
	}
	return entry.URL, entry.Source, true, nil
}

func (c *RedisArtworkCache) Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url, source string) error {
	if c.client == nil {
		return nil
	}

	payload, err := json.Marshal(artworkEntry{URL: url, Source: source})
	if err != nil {
		return err
	}

	key := artworkCacheKey(kind, title, subtitle, mbid)
	ttl := artworkPositiveTTL
	if url == "" {
		ttl = negativeTTL(kind)
	}

	return c.client.Set(ctx, key, payload, ttl).Err()
}

func artworkCacheKey(kind domain.ResultKind, title, subtitle, mbid string) string {
	// v3: value is now JSON {url, source} (was a bare URL string) — the bump
	// abandons v2 entries so a hit can't decode-fail on the old format; stale keys
	// expire by TTL. (v2 introduced identity-aware keying — the "Che" same-name
	// fix.) Bump this on any change to how artwork is resolved or stored.
	input := fmt.Sprintf("%s|%s|%s", title, subtitle, mbid)
	return hashKey("discovery:artwork:v3:"+kind.String()+":", input)
}

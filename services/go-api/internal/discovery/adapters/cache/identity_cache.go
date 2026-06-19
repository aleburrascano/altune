package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	identityPositiveTTL = 30 * 24 * time.Hour // confirmed/contamination
	identityUnknownTTL  = 24 * time.Hour      // suspect/unknown
)

type identityCacheEntry struct {
	Verdict   string    `json:"verdict"`
	Reason    string    `json:"reason"`
	Layer     string    `json:"layer"`
	FirstSeen time.Time `json:"first_seen"`
}

type IdentityCache struct {
	client *goredis.Client
}

func NewIdentityCache(client *goredis.Client) *IdentityCache {
	return &IdentityCache{client: client}
}

func (c *IdentityCache) Get(ctx context.Context, artistName, albumTitle string) (*identityCacheEntry, bool) {
	if c.client == nil {
		return nil, false
	}
	key := identityCacheKey(artistName, albumTitle)
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}
	var entry identityCacheEntry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		slog.WarnContext(ctx, "identity_cache.unmarshal_failed", "key", key, "error", err)
		return nil, false
	}
	return &entry, true
}

func (c *IdentityCache) Set(ctx context.Context, artistName, albumTitle string, entry identityCacheEntry) {
	if c.client == nil {
		return
	}
	key := identityCacheKey(artistName, albumTitle)
	data, err := json.Marshal(entry)
	if err != nil {
		slog.WarnContext(ctx, "identity_cache.marshal_failed", "key", key, "error", err)
		return
	}
	ttl := identityTTL(entry.Verdict)
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.WarnContext(ctx, "identity_cache.set_failed", "key", key, "error", err)
	}
}

func identityTTL(verdict string) time.Duration {
	switch verdict {
	case "confirmed", "contamination":
		return identityPositiveTTL
	default:
		return identityUnknownTTL
	}
}

func identityCacheKey(artistName, albumTitle string) string {
	h := sha256.Sum256([]byte(artistName + "|" + albumTitle))
	return fmt.Sprintf("discovery:identity:v1:%x", h[:16])
}

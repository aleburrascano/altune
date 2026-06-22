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

// RedisEnrichmentCache is also the IdentityBridge read side — the cross-provider
// ids it caches per (kind, mbid) are exactly the bridge merge consults — and the
// MBIDIndex, the name→MBID memo the search path reads to attach MBIDs to non-MB
// results.
var (
	_ ports.IdentityBridge = (*RedisEnrichmentCache)(nil)
	_ ports.MBIDIndex      = (*RedisEnrichmentCache)(nil)
)

const (
	enrichmentPositiveTTL = 14 * 24 * time.Hour
	enrichmentNegativeTTL = 24 * time.Hour
	enrichmentNegValue    = "1"
)

// RedisEnrichmentCache is a read-through cache of MBEnrichment. Positive entries
// store the whole value object (artwork included) keyed by (kind, mbid); the
// negative path records that a (kind, name) resolved to nothing. A nil client is
// a no-op (Get miss / Set no-op), so the service runs uncached without Redis.
type RedisEnrichmentCache struct {
	client *goredis.Client
}

func NewRedisEnrichmentCache(client *goredis.Client) *RedisEnrichmentCache {
	return &RedisEnrichmentCache{client: client}
}

func (c *RedisEnrichmentCache) Get(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error) {
	if c.client == nil {
		return domain.EmptyEnrichment(), false, nil
	}
	val, err := c.client.Get(ctx, enrichmentKey(kind, mbid)).Result()
	if err != nil {
		return domain.EmptyEnrichment(), false, nil
	}
	var e domain.MBEnrichment
	if err := json.Unmarshal([]byte(val), &e); err != nil {
		return domain.EmptyEnrichment(), false, nil
	}
	return e, true, nil
}

func (c *RedisEnrichmentCache) Set(ctx context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error {
	if c.client == nil {
		return nil
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, enrichmentKey(kind, mbid), blob, enrichmentPositiveTTL).Err()
}

func (c *RedisEnrichmentCache) GetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) (bool, error) {
	if c.client == nil {
		return false, nil
	}
	_, err := c.client.Get(ctx, enrichmentNegKey(kind, nameKey)).Result()
	return err == nil, nil
}

func (c *RedisEnrichmentCache) SetNegative(ctx context.Context, kind domain.ResultKind, nameKey string) error {
	if c.client == nil {
		return nil
	}
	return c.client.Set(ctx, enrichmentNegKey(kind, nameKey), enrichmentNegValue, enrichmentNegativeTTL).Err()
}

// ExternalIDs implements ports.IdentityBridge: it returns the cross-provider ids
// MusicBrainz asserts for a resolved entity, read from the positive enrichment
// cache (the same (kind, mbid) entry Set writes on detail-open — no extra MB
// round-trip). A miss (never enriched, or the entity carries no ids) reports
// (nil, false) so merge falls back to name similarity.
func (c *RedisEnrichmentCache) ExternalIDs(ctx context.Context, kind domain.ResultKind, mbid string) (map[string]string, bool) {
	if c.client == nil || mbid == "" {
		return nil, false
	}
	e, found, _ := c.Get(ctx, kind, mbid)
	if !found || len(e.ExternalIDs) == 0 {
		return nil, false
	}
	return e.ExternalIDs, true
}

// LookupMBID implements ports.MBIDIndex: it reads a remembered name→MBID. A miss
// (or no Redis) reports ("", false) so the search path leaves the result's own
// thumbnail in place.
func (c *RedisEnrichmentCache) LookupMBID(ctx context.Context, kind domain.ResultKind, nameKey string) (string, bool) {
	if c.client == nil || nameKey == "" {
		return "", false
	}
	val, err := c.client.Get(ctx, mbidIndexKey(kind, nameKey)).Result()
	if err != nil || val == "" {
		return "", false
	}
	return val, true
}

// RememberMBID implements ports.MBIDIndex: it memoizes a strict name resolution
// so a later search can attach the MBID without an MB call. nil client / empty
// inputs no-op.
func (c *RedisEnrichmentCache) RememberMBID(ctx context.Context, kind domain.ResultKind, nameKey, mbid string) error {
	if c.client == nil || nameKey == "" || mbid == "" {
		return nil
	}
	return c.client.Set(ctx, mbidIndexKey(kind, nameKey), mbid, enrichmentPositiveTTL).Err()
}

func mbidIndexKey(kind domain.ResultKind, nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:mbid:v1:%s:%x", kind.String(), h[:16])
}

func enrichmentKey(kind domain.ResultKind, mbid string) string {
	return fmt.Sprintf("discovery:mbenrich:v1:%s:%s", kind.String(), mbid)
}

func enrichmentNegKey(kind domain.ResultKind, nameKey string) string {
	h := sha256.Sum256([]byte(nameKey))
	return fmt.Sprintf("discovery:mbenrich:neg:v1:%s:%x", kind.String(), h[:16])
}

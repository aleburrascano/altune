package cache

import (
	"context"
	"encoding/json"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.IdentityStore = (*RedisIdentityStore)(nil)

// identityTTL bounds how long a bridged identity stays warm in Redis. Identity is
// effectively immutable (a provider id maps to one MB entity), so this is long;
// Postgres remains the source of truth, so an eviction only costs one PG read.
const identityTTL = 30 * 24 * time.Hour

// RedisIdentityStore is a read-through/write-through cache in front of a durable
// IdentityStore (the Postgres adapter). It keeps the per-result identity lookup
// off the database on the hot search path while Postgres guarantees durability —
// a Redis flush simply re-warms from Postgres on the next lookup. When the Redis
// client is nil it transparently delegates to the inner store (graceful
// degradation: correctness without the cache).
type RedisIdentityStore struct {
	inner  ports.IdentityStore
	client *goredis.Client
}

func NewRedisIdentityStore(inner ports.IdentityStore, client *goredis.Client) *RedisIdentityStore {
	return &RedisIdentityStore{inner: inner, client: client}
}

type identityEntry struct {
	MBID string            `json:"mbid"`
	Xref map[string]string `json:"xref"`
}

// PersistBridges writes through: durable store first (source of truth), then warm
// each provider id's cache entry so the very next search reads from Redis.
func (s *RedisIdentityStore) PersistBridges(
	ctx context.Context,
	kind domain.ResultKind,
	mbid string,
	xref map[string]string,
) error {
	if err := s.inner.PersistBridges(ctx, kind, mbid, xref); err != nil {
		return err
	}
	if s.client == nil || mbid == "" {
		return nil
	}
	blob, err := json.Marshal(identityEntry{MBID: mbid, Xref: xref})
	if err != nil {
		return nil // cache warming is best-effort; the durable write already succeeded
	}
	for provider, externalID := range xref {
		if provider == "" || externalID == "" {
			continue
		}
		_ = s.client.Set(ctx, identityKey(kind, provider, externalID), blob, identityTTL).Err()
	}
	return nil
}

// LookupByProviderID reads Redis first, falling through to the durable store on a
// miss and back-filling the cache. Any Redis error degrades to the durable read.
func (s *RedisIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider, externalID string,
) (string, map[string]string, bool) {
	if provider == "" || externalID == "" {
		return "", nil, false
	}
	key := identityKey(kind, provider, externalID)
	if s.client != nil {
		if val, err := s.client.Get(ctx, key).Result(); err == nil {
			var e identityEntry
			if json.Unmarshal([]byte(val), &e) == nil && e.MBID != "" {
				return e.MBID, e.Xref, true
			}
		}
	}

	mbid, xref, ok := s.inner.LookupByProviderID(ctx, kind, provider, externalID)
	if !ok {
		return "", nil, false
	}
	if s.client != nil {
		if blob, err := json.Marshal(identityEntry{MBID: mbid, Xref: xref}); err == nil {
			_ = s.client.Set(ctx, key, blob, identityTTL).Err()
		}
	}
	return mbid, xref, true
}

func identityKey(kind domain.ResultKind, provider, externalID string) string {
	return hashKey("discovery:identity:v1:"+kind.String()+":", provider+"|"+externalID)
}

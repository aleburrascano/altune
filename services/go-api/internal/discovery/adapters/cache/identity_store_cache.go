package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.IdentityStore = (*RedisIdentityStore)(nil)

const identityTTL = 30 * 24 * time.Hour

type RedisIdentityStore struct {
	inner ports.IdentityStore
	redisJSON
}

func NewRedisIdentityStore(inner ports.IdentityStore, client *goredis.Client, opts ...Option) *RedisIdentityStore {
	return &RedisIdentityStore{inner: inner, redisJSON: newRedisJSON(client, opts)}
}

type identityEntry struct {
	MBID string            `json:"mbid"`
	Xref map[string]string `json:"xref"`
}

func (s *RedisIdentityStore) PersistBridges(
	ctx context.Context,
	kind domain.ResultKind,
	mbid string,
	xref map[string]string,
) error {
	if err := s.inner.PersistBridges(ctx, kind, mbid, xref); err != nil {
		return err
	}
	if s.disabled() || mbid == "" {
		return nil
	}
	blob, err := json.Marshal(identityEntry{MBID: mbid, Xref: xref})
	if err != nil {
		// The inner store (source of truth) already persisted above; failing to
		// marshal the cache-warm blob only skips warming, so report success.
		return nil //nolint:nilerr // cache warming is best-effort after the durable write
	}
	for provider, externalID := range xref {
		if provider == "" || externalID == "" {
			continue
		}
		if err := s.client.Set(ctx, identityKey(kind, domain.ProviderKey(provider), externalID), blob, identityTTL).Err(); err != nil {
			slog.DebugContext(ctx, "identity.cache_warm_failed",
				"kind", kind.String(), "provider", provider, "error", err)
		}
	}
	return nil
}

func (s *RedisIdentityStore) Invalidate(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) error {
	err := s.inner.Invalidate(ctx, kind, provider, externalID)
	if !s.disabled() && provider != "" && externalID != "" {
		if delErr := s.client.Del(ctx, identityKey(kind, provider, externalID)).Err(); delErr != nil {
			slog.DebugContext(ctx, "identity.cache_invalidate_failed",
				"kind", kind.String(), "provider", provider.String(), "error", delErr)
		}
	}
	return err
}

func (s *RedisIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) (string, map[string]string, bool) {
	if provider == "" || externalID == "" {
		return "", nil, false
	}
	key := identityKey(kind, provider, externalID)
	if !s.disabled() {
		if e, ok := getJSON[identityEntry](ctx, s.redisJSON, key); ok && e.MBID != "" {
			return e.MBID, e.Xref, true
		}
	}

	mbid, xref, ok := s.inner.LookupByProviderID(ctx, kind, provider, externalID)
	if !ok {
		return "", nil, false
	}
	if !s.disabled() {
		_ = s.setJSON(ctx, key, identityEntry{MBID: mbid, Xref: xref}, identityTTL)
	}
	return mbid, xref, true
}

func identityKey(kind domain.ResultKind, provider domain.ProviderKey, externalID string) string {
	return hashKey("discovery:identity:v1:"+kind.String()+":", provider.String()+"|"+externalID)
}

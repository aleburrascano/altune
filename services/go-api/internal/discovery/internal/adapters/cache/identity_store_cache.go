package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/core/ports"
	"context"
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
	MBID string                        `json:"mbid"`
	Xref map[domain.ProviderKey]string `json:"xref"`
}

func (s *RedisIdentityStore) LookupByProviderID(
	ctx context.Context,
	kind domain.ResultKind,
	provider domain.ProviderKey, externalID string,
) (string, map[domain.ProviderKey]string, bool) {
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
	s.backfill(ctx, key, identityEntry{MBID: mbid, Xref: xref})
	return mbid, xref, true
}

func (s *RedisIdentityStore) backfill(ctx context.Context, key string, entry identityEntry) {
	if s.disabled() {
		return
	}
	if err := s.setJSON(ctx, key, entry, identityTTL); err != nil {
		slog.DebugContext(ctx, "identity.cache_backfill_failed", "error", err)
	}
}

func identityKey(kind domain.ResultKind, provider domain.ProviderKey, externalID string) string {
	return hashKey("discovery:identity:v1:"+kind.String()+":", provider.String()+"|"+externalID)
}

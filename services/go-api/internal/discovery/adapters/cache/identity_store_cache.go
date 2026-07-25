package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

var _ ports.IdentityStore = (*RedisIdentityStore)(nil)

const identityTTL = 30 * 24 * time.Hour

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
		return nil
	}
	for provider, externalID := range xref {
		if provider == "" || externalID == "" {
			continue
		}
		if err := s.client.Set(ctx, identityKey(kind, provider, externalID), blob, identityTTL).Err(); err != nil {
			slog.DebugContext(ctx, "identity.cache_warm_failed",
				"kind", kind.String(), "provider", provider, "error", err)
		}
	}
	return nil
}

func (s *RedisIdentityStore) Invalidate(
	ctx context.Context,
	kind domain.ResultKind,
	provider, externalID string,
) error {
	err := s.inner.Invalidate(ctx, kind, provider, externalID)
	if s.client != nil && provider != "" && externalID != "" {
		if delErr := s.client.Del(ctx, identityKey(kind, provider, externalID)).Err(); delErr != nil {
			slog.DebugContext(ctx, "identity.cache_invalidate_failed",
				"kind", kind.String(), "provider", provider, "error", delErr)
		}
	}
	return err
}

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

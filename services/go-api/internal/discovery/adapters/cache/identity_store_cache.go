package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"errors"
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
	for provider, externalID := range xref {
		if provider == "" || externalID == "" {
			continue
		}
		if err := s.dropCached(ctx, identityKey(kind, domain.ProviderKey(provider), externalID)); err != nil {
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
		if delErr := s.dropCached(ctx, identityKey(kind, provider, externalID)); delErr != nil {
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

	seenGen := s.readGen(ctx, key)
	mbid, xref, ok := s.inner.LookupByProviderID(ctx, kind, provider, externalID)
	if !ok {
		return "", nil, false
	}
	if !s.disabled() {
		s.backfill(ctx, key, seenGen, identityEntry{MBID: mbid, Xref: xref})
	}
	return mbid, xref, true
}

func identityGenKey(key string) string { return key + ":gen" }

func (s *RedisIdentityStore) dropCached(ctx context.Context, key string) error {
	genKey := identityGenKey(key)
	_, err := s.client.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		pipe.Del(ctx, key)
		pipe.Incr(ctx, genKey)
		pipe.Expire(ctx, genKey, identityTTL)
		return nil
	})
	return err
}

func (s *RedisIdentityStore) readGen(ctx context.Context, key string) string {
	if s.disabled() {
		return ""
	}
	gen, err := s.client.Get(ctx, identityGenKey(key)).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		slog.DebugContext(ctx, "identity.cache_gen_read_failed", "error", err)
	}
	return gen
}

func (s *RedisIdentityStore) backfill(ctx context.Context, key, seenGen string, entry identityEntry) {
	blob, err := json.Marshal(entry)
	if err != nil {
		return
	}
	genKey := identityGenKey(key)
	err = s.client.Watch(ctx, func(tx *goredis.Tx) error {
		gen, err := tx.Get(ctx, genKey).Result()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return err
		}
		if gen != seenGen {
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
			return pipe.Set(ctx, key, blob, identityTTL).Err()
		})
		return err
	}, genKey)
	if err != nil && !errors.Is(err, goredis.TxFailedErr) {
		slog.DebugContext(ctx, "identity.cache_backfill_failed", "error", err)
	}
}

func identityKey(kind domain.ResultKind, provider domain.ProviderKey, externalID string) string {
	return hashKey("discovery:identity:v1:"+kind.String()+":", provider.String()+"|"+externalID)
}

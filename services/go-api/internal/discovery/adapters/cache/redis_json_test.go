package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
)

func TestCaches_UnreachableRedis_ReadsDegrade(t *testing.T) {
	client := unreachableRedisClient(t)
	ctx := context.Background()

	t.Run("artwork Get is a clean miss", func(t *testing.T) {
		c := NewRedisArtworkCache(client)
		if _, _, hit, err := c.Get(ctx, domain.ResultKindTrack, "t", "s", ""); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
	})

	t.Run("enrichment Get and GetNegative are clean misses", func(t *testing.T) {
		c := NewRedisEnrichmentCache(client)
		if _, hit, err := c.Get(ctx, domain.ResultKindAlbum, "mbid"); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
		if neg, err := c.GetNegative(ctx, domain.ResultKindAlbum, "name"); neg || err == nil {
			t.Errorf("GetNegative = (%v,%v), want (false, non-nil error)", neg, err)
		}
		if _, ok := c.LookupMBID(ctx, domain.ResultKindAlbum, "name"); ok {
			t.Error("LookupMBID hit on unreachable Redis, want miss")
		}
	})

	t.Run("result cache Get is a clean miss", func(t *testing.T) {
		c := NewRedisResultCache(client)
		if got, hit := c.Get(ctx, "key"); hit || got != nil {
			t.Errorf("Get = (%v,%v), want clean miss", got, hit)
		}
	})

	t.Run("name-keyed Get and GetNegative are clean misses", func(t *testing.T) {
		c := NewRedisLastFmEnrichmentCache(client)
		if _, hit, err := c.Get(ctx, "name"); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
		if neg, err := c.GetNegative(ctx, "name"); neg || err == nil {
			t.Errorf("GetNegative = (%v,%v), want (false, non-nil error)", neg, err)
		}
	})

	t.Run("identity lookup falls through to the durable store", func(t *testing.T) {
		inner := &recordingIdentityStore{
			mbid: "mbid-2", xref: map[string]string{"spotify": "s1"}, found: true,
		}
		store := NewRedisIdentityStore(inner, client)
		mbid, _, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "spotify", "s1")
		if !ok || mbid != "mbid-2" {
			t.Errorf("lookup = (%q,%v), want durable-store fallthrough", mbid, ok)
		}
		if inner.lookupCalls != 1 {
			t.Errorf("durable lookup calls = %d, want 1", inner.lookupCalls)
		}
	})
}

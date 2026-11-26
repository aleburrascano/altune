package cache_test

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRedisTrackerIndexCache_DisabledClientNeverHits(t *testing.T) {
	c := cache.NewRedisTrackerIndexCache(nil)
	ctx := context.Background()
	if err := c.Set(ctx, map[string]string{"a": "b"}, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := c.Get(ctx); ok {
		t.Error("Get hit on a disabled cache")
	}
}

func TestRedisTrackerIndexCache_RoundTripsWithTheGivenTTL(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisTrackerIndexCache(client)
	ctx := context.Background()
	want := map[string]string{"ken carson": "1OARID98"}

	if err := c.Set(ctx, want, 24*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := c.Get(ctx)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Get = (%v, %v), want (%v, true)", got, ok, want)
	}
	assertStored(t, server, "trackers:hub:v1", 24*time.Hour)
}

func TestRedisTrackerIndexCache_AnEmptyCacheIsAMiss(t *testing.T) {
	_, client := newRedis(t)
	if got, ok := cache.NewRedisTrackerIndexCache(client).Get(context.Background()); ok {
		t.Errorf("Get = (%v, true) on an empty Redis, want a miss", got)
	}
}

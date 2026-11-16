package cache

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRedisTrackerIndexCache_DisabledClientNeverHits(t *testing.T) {
	c := NewRedisTrackerIndexCache(nil)
	ctx := context.Background()
	if err := c.Set(ctx, map[string]string{"a": "b"}, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := c.Get(ctx); ok {
		t.Error("Get hit on a disabled cache")
	}
}

func TestRedisTrackerIndexCache_RoundTripsWithTheGivenTTL(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisTrackerIndexCache(client)
	ctx := context.Background()
	cleanKeys(t, client, trackerIndexKey)
	want := map[string]string{"ken carson": "1OARID98"}
	if err := c.Set(ctx, want, 24*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := c.Get(ctx)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Get = (%v, %v), want (%v, true)", got, ok, want)
	}
	if ttl := client.TTL(ctx, trackerIndexKey).Val(); ttl <= 0 || ttl > 24*time.Hour {
		t.Errorf("ttl = %v, want within (0, 24h]", ttl)
	}
}

package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func testRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set, skipping Redis integration test")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_URL: %v", err)
	}
	client := goredis.NewClient(opts)
	t.Cleanup(func() { client.Close() })
	return client
}

func cleanKeys(t *testing.T, client *goredis.Client, keys ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, k := range keys {
			client.Del(ctx, k)
		}
	})
}

func unreachableRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:            "127.0.0.1:1",
		DialTimeout:     200 * time.Millisecond,
		ReadTimeout:     200 * time.Millisecond,
		WriteTimeout:    200 * time.Millisecond,
		MaxRetries:      -1,
		PoolTimeout:     200 * time.Millisecond,
		MinRetryBackoff: -1,
		MaxRetryBackoff: -1,
	})
	t.Cleanup(func() { client.Close() })
	return client
}

type recordingIdentityStore struct {
	mbid  string
	xref  map[string]string
	found bool

	persistCalls    int
	lookupCalls     int
	invalidateCalls int
	invalidateErr   error
}

func (f *recordingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	f.persistCalls++
	return nil
}

func (f *recordingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[string]string, bool) {
	f.lookupCalls++
	return f.mbid, f.xref, f.found
}

func (f *recordingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	f.invalidateCalls++
	return f.invalidateErr
}

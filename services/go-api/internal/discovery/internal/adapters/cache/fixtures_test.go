package cache_test

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newRedis(t *testing.T) (*miniredis.Miniredis, *goredis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func secondClient(t *testing.T, server *miniredis.Miniredis) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func storedKeys(server *miniredis.Miniredis) []string {
	keys := server.Keys()
	slices.Sort(keys)
	return keys
}

func assertStored(t *testing.T, server *miniredis.Miniredis, key string, ttl time.Duration) {
	t.Helper()
	if !server.Exists(key) {
		t.Fatalf("key %q not stored, have %v", key, storedKeys(server))
	}
	if got := server.TTL(key); got != ttl {
		t.Errorf("TTL of %q = %v, want %v", key, got, ttl)
	}
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
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type recordingIdentityStore struct {
	mbid  string
	xref  map[domain.ProviderKey]string
	found bool

	mu          sync.Mutex
	lookupCalls int
}

func (f *recordingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[domain.ProviderKey]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookupCalls++
	return f.mbid, f.xref, f.found
}

func (f *recordingIdentityStore) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookupCalls
}

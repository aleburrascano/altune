package cache

import (
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

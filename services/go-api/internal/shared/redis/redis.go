package redis

import (
	"context"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func NewClient(ctx context.Context, redisURL string) *goredis.Client {
	if redisURL == "" {
		slog.Info("redis not configured, caches will degrade gracefully")
		return nil
	}

	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		slog.Warn("invalid redis URL, caches will degrade gracefully", "error", err)
		return nil
	}

	client := goredis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		slog.Warn("redis not reachable, caches will degrade gracefully", "error", err)
		return client
	}

	slog.Info("redis connected")
	return client
}

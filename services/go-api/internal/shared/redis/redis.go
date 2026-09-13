package redis

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
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
		slog.Warn("invalid redis URL, caches will degrade gracefully", "error", redactURLError(err))
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

// redactURLError strips the original URL from a *url.Error, whose Error()
// string embeds the raw input (credentials included), keeping only the reason.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

package redis

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const defaultPoolSize = 50

func NewClient(ctx context.Context, redisURL string, poolSize int) *goredis.Client {
	if redisURL == "" {
		slog.Info("redis not configured, caches will degrade gracefully")
		return nil
	}

	opts, err := clientOptions(redisURL, poolSize)
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

func ValidateURL(redisURL string) error {
	if _, err := clientOptions(redisURL, 0); err != nil {
		return redactURLError(err)
	}
	return nil
}

func clientOptions(redisURL string, poolSize int) (*goredis.Options, error) {
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	if poolSize <= 0 {
		poolSize = defaultPoolSize
	}
	opts.PoolSize = poolSize
	return opts, nil
}

type PoolStats struct {
	PoolSize   int    `json:"pool_size"`
	TotalConns uint32 `json:"total_conns"`
	IdleConns  uint32 `json:"idle_conns"`
	Timeouts   uint32 `json:"timeouts"`
}

func ReadPoolStats(client *goredis.Client) PoolStats {
	if client == nil {
		return PoolStats{}
	}
	stats := client.PoolStats()
	return PoolStats{
		PoolSize:   client.Options().PoolSize,
		TotalConns: stats.TotalConns,
		IdleConns:  stats.IdleConns,
		Timeouts:   stats.Timeouts,
	}
}

func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

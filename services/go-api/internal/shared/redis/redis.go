package redis

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// defaultPoolSize is the connection ceiling applied when the caller passes a
// non-positive REDIS_POOL_SIZE. go-redis's own default is 10 x GOMAXPROCS,
// which makes the ceiling a property of the container shape; the fallback is a
// fixed number so a misconfiguration cannot silently reintroduce that.
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

// PoolStats is a point-in-time read of the client pool's saturation. Timeouts
// is the saturation signal: it advances only when a caller waited for a
// connection and gave up because all PoolSize of them were busy.
type PoolStats struct {
	PoolSize   int    `json:"pool_size"`
	TotalConns uint32 `json:"total_conns"`
	IdleConns  uint32 `json:"idle_conns"`
	Timeouts   uint32 `json:"timeouts"`
}

// ReadPoolStats reads the counters without blocking the pool, so the fields can
// be marginally inconsistent with each other — acceptable for an operator view.
// A nil client (redis not configured, the degraded-cache path) reads as a zero
// value.
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

// redactURLError strips the original URL from a *url.Error, whose Error()
// string embeds the raw input (credentials included), keeping only the reason.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

package cache

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const redisNegSentinel = "1"

type redisJSON struct {
	client *goredis.Client
}

func (r redisJSON) disabled() bool { return r.client == nil }

func getJSON[T any](ctx context.Context, r redisJSON, key string) (T, bool) {
	var zero T
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return zero, false
	}
	var v T
	if err := json.Unmarshal([]byte(val), &v); err != nil {
		return zero, false
	}
	return v, true
}

func (r redisJSON) setJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	blob, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, blob, ttl).Err()
}

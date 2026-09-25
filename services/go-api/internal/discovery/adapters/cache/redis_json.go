package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const redisNegSentinel = "1"

type redisJSON struct {
	client *goredis.Client
	signal *Signal
}

func newRedisJSON(client *goredis.Client, opts []Option) redisJSON {
	r := redisJSON{client: client, signal: newDefaultSignal()}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func (r redisJSON) disabled() bool { return r.client == nil }

func getJSON[T any](ctx context.Context, r redisJSON, key string) (T, bool) {
	var zero T
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		r.recordReadFailure(ctx, key, err)
		return zero, false
	}
	var v T
	if err := json.Unmarshal([]byte(val), &v); err != nil {
		r.signal.failure(ctx, kindOf(key), opDecode, err)
		return zero, false
	}
	r.signal.count(kindOf(key), outcomeHit)
	return v, true
}

func (r redisJSON) recordReadFailure(ctx context.Context, key string, err error) {
	if errors.Is(err, goredis.Nil) {
		r.signal.count(kindOf(key), outcomeMiss)
		return
	}
	r.signal.failure(ctx, kindOf(key), opGet, err)
}

func (r redisJSON) getNegative(ctx context.Context, key string) (bool, error) {
	_, err := r.client.Get(ctx, key).Result()
	if err == nil {
		r.signal.count(kindOf(key), outcomeHit)
		return true, nil
	}
	r.recordReadFailure(ctx, key, err)
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	return false, err
}

func (r redisJSON) setJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	blob, err := json.Marshal(v)
	if err != nil {
		r.signal.failure(ctx, kindOf(key), opEncode, err)
		return err
	}
	return r.setRaw(ctx, key, blob, ttl)
}

func (r redisJSON) setRaw(ctx context.Context, key string, val any, ttl time.Duration) error {
	err := r.client.Set(ctx, key, val, ttl).Err()
	if err != nil {
		r.signal.failure(ctx, kindOf(key), opSet, err)
	}
	return err
}

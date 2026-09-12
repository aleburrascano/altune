package providers

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type cachedResolver[T comparable] struct {
	key       string
	timeout   time.Duration
	resolveFn func(context.Context) (T, time.Time, error)
	validFn   func(T) bool

	mu     sync.Mutex
	sf     singleflight.Group
	cached T
	expiry time.Time
}

func newCachedResolver[T comparable](
	key string,
	timeout time.Duration,
	resolve func(context.Context) (T, time.Time, error),
	valid func(T) bool,
) *cachedResolver[T] {
	return &cachedResolver[T]{key: key, timeout: timeout, resolveFn: resolve, validFn: valid}
}

func (r *cachedResolver[T]) get(ctx context.Context) (T, error) {
	r.mu.Lock()
	cached, expiry := r.cached, r.expiry
	r.mu.Unlock()
	if r.fresh(cached, expiry) {
		return cached, nil
	}

	v, err, _ := r.sf.Do(r.key, func() (any, error) {
		r.mu.Lock()
		existing, existingExpiry := r.cached, r.expiry
		r.mu.Unlock()
		if r.fresh(existing, existingExpiry) {
			return existing, nil
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
		defer cancel()
		value, valueExpiry, err := r.resolveFn(rctx)
		if err != nil {
			return value, err
		}
		r.mu.Lock()
		r.cached, r.expiry = value, valueExpiry
		r.mu.Unlock()
		return value, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

func (r *cachedResolver[T]) fresh(cached T, expiry time.Time) bool {
	return r.validFn(cached) && (expiry.IsZero() || time.Now().Before(expiry))
}

func (r *cachedResolver[T]) invalidate(failed T) {
	r.mu.Lock()
	if r.cached == failed {
		var zero T
		r.cached = zero
	}
	r.mu.Unlock()
}

// withAuthRetry runs do with a credential resolved from r, retrying exactly once on an
// auth-status failure. It is the single owner of the invalidate-reget-retry-once policy:
// the read-side complement to cachedResolver.invalidate. On the first call that fails with
// an auth status it invalidates the spent credential, re-resolves a fresh one, and calls do
// a second time; any other failure (or a re-resolve failure) is returned as-is without retry.
func withAuthRetry[T comparable, R any](
	ctx context.Context,
	r *cachedResolver[T],
	do func(context.Context, T) (R, int, error),
) (R, error) {
	cred, err := r.get(ctx)
	if err != nil {
		var zero R
		return zero, err
	}
	res, status, err := do(ctx, cred)
	if err != nil && isAuthStatus(status) {
		r.invalidate(cred)
		cred, err = r.get(ctx)
		if err != nil {
			var zero R
			return zero, err
		}
		res, _, err = do(ctx, cred)
	}
	return res, err
}

func nonEmpty(s string) bool { return s != "" }

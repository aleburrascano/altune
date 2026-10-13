package providerhttp

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type CachedResolver[T comparable] struct {
	key       string
	timeout   time.Duration
	resolveFn func(context.Context) (T, time.Time, error)
	validFn   func(T) bool

	Mu     sync.Mutex
	sf     singleflight.Group
	Cached T
	Expiry time.Time
}

func NewCachedResolver[T comparable](
	key string,
	timeout time.Duration,
	resolve func(context.Context) (T, time.Time, error),
	valid func(T) bool,
) *CachedResolver[T] {
	return &CachedResolver[T]{key: key, timeout: timeout, resolveFn: resolve, validFn: valid}
}

func (r *CachedResolver[T]) Get(ctx context.Context) (T, error) {
	if cached, expiry := r.snapshot(); r.fresh(cached, expiry) {
		return cached, nil
	}
	ch := r.sf.DoChan(r.key, func() (any, error) { return r.resolveOnce(ctx) })
	return waitOrGiveUp[T](ctx, ch)
}

func (r *CachedResolver[T]) snapshot() (T, time.Time) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	return r.Cached, r.Expiry
}

func (r *CachedResolver[T]) resolveOnce(ctx context.Context) (T, error) {
	if existing, existingExpiry := r.snapshot(); r.fresh(existing, existingExpiry) {
		return existing, nil
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
	defer cancel()
	value, valueExpiry, err := r.resolveFn(rctx)
	if err != nil {
		return value, err
	}
	r.Mu.Lock()
	r.Cached, r.Expiry = value, valueExpiry
	r.Mu.Unlock()
	return value, nil
}

func waitOrGiveUp[T any](ctx context.Context, ch <-chan singleflight.Result) (T, error) {
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			var zero T
			return zero, res.Err
		}
		return res.Val.(T), nil
	}
}

func (r *CachedResolver[T]) fresh(cached T, expiry time.Time) bool {
	return r.validFn(cached) && (expiry.IsZero() || time.Now().Before(expiry))
}

func (r *CachedResolver[T]) Invalidate(failed T) {
	r.Mu.Lock()
	if r.Cached == failed {
		var zero T
		r.Cached = zero
	}
	r.Mu.Unlock()
}

func WithAuthRetry[T comparable, R any](
	ctx context.Context,
	r *CachedResolver[T],
	do func(context.Context, T) (R, int, error),
) (R, error) {
	cred, err := r.Get(ctx)
	if err != nil {
		var zero R
		return zero, err
	}
	res, status, err := do(ctx, cred)
	if err != nil && IsAuthStatus(status) {
		r.Invalidate(cred)
		return retryWithFreshCredential(ctx, r, do)
	}
	return res, err
}

func retryWithFreshCredential[T comparable, R any](
	ctx context.Context,
	r *CachedResolver[T],
	do func(context.Context, T) (R, int, error),
) (R, error) {
	cred, err := r.Get(ctx)
	if err != nil {
		var zero R
		return zero, err
	}
	res, status, err := do(ctx, cred)
	if err != nil && IsAuthStatus(status) {
		err = PersistentAuthError{Cause: err}
	}
	return res, err
}

func NonEmpty(s string) bool { return s != "" }

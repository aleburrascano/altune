package providers

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
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
	now       clock.Func

	mu     sync.Mutex
	sf     singleflight.Group
	cached T
	expiry time.Time
}

func NewCachedResolver[T comparable](
	key string,
	timeout time.Duration,
	resolve func(context.Context) (T, time.Time, error),
	valid func(T) bool,
	now clock.Func,
) *CachedResolver[T] {
	if now == nil {
		panic("providerrequests.NewCachedResolver: a clock is required")
	}
	return &CachedResolver[T]{key: key, timeout: timeout, resolveFn: resolve, validFn: valid, now: now}
}

func (r *CachedResolver[T]) Get(ctx context.Context) (T, error) {
	if cached, expiry := r.snapshot(); r.fresh(cached, expiry) {
		return cached, nil
	}
	ch := r.sf.DoChan(r.key, func() (any, error) { return r.resolveOnce(ctx) })
	return waitOrGiveUp[T](ctx, ch)
}

func (r *CachedResolver[T]) snapshot() (T, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cached, r.expiry
}

func (r *CachedResolver[T]) resolveOnce(ctx context.Context) (T, error) {
	if existing, existingExpiry := r.snapshot(); r.fresh(existing, existingExpiry) {
		return existing, nil
	}
	rctx, cancel := background.Detach(ctx, r.timeout)
	defer cancel()
	value, valueExpiry, err := r.resolveFn(rctx)
	if err != nil {
		return value, err
	}
	r.mu.Lock()
	r.cached, r.expiry = value, valueExpiry
	r.mu.Unlock()
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
	return r.validFn(cached) && (expiry.IsZero() || r.now().Before(expiry))
}

func (r *CachedResolver[T]) Invalidate(failed T) {
	r.mu.Lock()
	if r.cached == failed {
		var zero T
		r.cached = zero
	}
	r.mu.Unlock()
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
	if err != nil && outbound.IsAuthStatus(status) {
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
	if err != nil && outbound.IsAuthStatus(status) {
		err = outbound.PersistentAuthError{Cause: err}
	}
	return res, err
}

func NonEmpty(s string) bool { return s != "" }

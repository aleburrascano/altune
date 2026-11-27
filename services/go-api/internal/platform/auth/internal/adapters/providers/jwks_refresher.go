package providers

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const jwksFlightKey = "jwks"

const jwksRefreshBackoffBase = time.Second

const jwksRefreshBackoffCap = 30 * time.Second

const jwksUnknownKeyRefreshInterval = 30 * time.Second

const defaultJWKSRefreshInterval = 15 * time.Minute

const defaultJWKSRefreshWindow = time.Minute

const jwksStaleAfter = time.Hour

var ErrJWKSStale = errors.New("JWKS key set is stale")

var errJWKSRefreshRecent = errors.New("JWKS refreshed recently")

var errJWKSRefreshAborted = errors.New("JWKS refresh aborted before it finished")

var ErrJWKSRefreshBackoff = errors.New("JWKS refresh backing off after failure")

type jwksRefresher struct {
	fetch        func(context.Context) error
	now          func() time.Time
	jitter       random.Unit
	fetchTimeout time.Duration

	flight singleflight.Group

	mu          sync.Mutex
	failures    int
	notBefore   time.Time
	lastErr     error
	lastSuccess time.Time

	keySetAt   time.Time
	bgFailures int
	bgErr      error
}

func newJWKSRefresher(fetch func(context.Context) error, now func() time.Time, jitter random.Unit, fetchTimeout time.Duration) *jwksRefresher {
	return &jwksRefresher{fetch: fetch, now: now, jitter: jitter, fetchTimeout: fetchTimeout}
}

func (r *jwksRefresher) Refresh(ctx context.Context) error {
	return r.refresh(ctx, 0)
}

func (r *jwksRefresher) RefreshIfStale(ctx context.Context) error {
	return r.refresh(ctx, jwksUnknownKeyRefreshInterval)
}

func (r *jwksRefresher) refresh(ctx context.Context, minAge time.Duration) error {
	pending := r.flight.DoChan(jwksFlightKey, func() (any, error) { return nil, r.fetchOnce(ctx, minAge) })
	select {
	case result := <-pending:
		return result.Err
	case <-ctx.Done():
		return fmt.Errorf("wait for JWKS refresh: %w", ctx.Err())
	}
}

func (r *jwksRefresher) fetchOnce(parent context.Context, minAge time.Duration) error {
	if err := r.admit(minAge); err != nil {
		return err
	}
	ctx, cancel := background.Detach(parent, r.fetchTimeout)
	defer cancel()
	err := errJWKSRefreshAborted
	_ = background.SafeRun(ctx, "auth.jwks_refresh_panic", func() { err = r.fetch(ctx) })
	r.settle(err)
	return err
}

func (r *jwksRefresher) admit(minAge time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.now().Sub(r.lastSuccess) < minAge {
		return errJWKSRefreshRecent
	}
	if wait := r.notBefore.Sub(r.now()); wait > 0 {
		return fmt.Errorf("%w (retry in %s): %w", ErrJWKSRefreshBackoff, wait.Round(time.Millisecond), r.lastErr)
	}
	return nil
}

func (r *jwksRefresher) settle(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.failures, r.notBefore, r.lastErr, r.lastSuccess = 0, time.Time{}, nil, r.now()
		return
	}
	r.failures++
	r.lastErr = err
	r.notBefore = r.now().Add(jwksRefreshBackoff.DelayAt(r.failures, r.jitter()))
}

func (r *jwksRefresher) recordKeySet() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keySetAt, r.bgFailures, r.bgErr = r.now(), 0, nil
}

func (r *jwksRefresher) recordBackgroundFailure(err error) (failures int, age time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bgFailures++
	r.bgErr = err
	if !r.keySetAt.IsZero() {
		age = r.now().Sub(r.keySetAt)
	}
	return r.bgFailures, age
}

func (r *jwksRefresher) checkFresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keySetAt.IsZero() {
		return nil
	}
	age := r.now().Sub(r.keySetAt)
	if age <= jwksStaleAfter {
		return nil
	}
	err := fmt.Errorf("%w: last successful refresh %s ago (bound %s), %d consecutive background refresh failures",
		ErrJWKSStale, age.Round(time.Second), jwksStaleAfter, r.bgFailures)
	cause := r.bgErr
	if cause == nil {
		cause = r.lastErr
	}
	if cause != nil {
		err = fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

var jwksRefreshBackoff = backoff.MustCapped(jwksRefreshBackoffBase, jwksRefreshBackoffCap)

package providers

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const jwksRefreshBackoffBase = time.Second

const jwksRefreshBackoffCap = 30 * time.Second

const jwksUnknownKeyRefreshInterval = 30 * time.Second

var jwksBackgroundRefreshInterval = 15 * time.Minute

var jwksRefreshWindow = time.Minute

const jwksStaleAfter = time.Hour

var errJWKSStale = errors.New("JWKS key set is stale")

var errJWKSRefreshRecent = errors.New("JWKS refreshed recently")

var errJWKSRefreshBackoff = errors.New("JWKS refresh backing off after failure")

type jwksRefresher struct {
	fetch  func(context.Context) error
	now    func() time.Time
	jitter func(time.Duration) time.Duration

	mu          sync.Mutex
	inflight    *jwksRefreshCall
	failures    int
	notBefore   time.Time
	lastErr     error
	lastSuccess time.Time

	keySetAt   time.Time
	bgFailures int
	bgErr      error
}

type jwksRefreshCall struct {
	done chan struct{}
	err  error
}

func newJWKSRefresher(fetch func(context.Context) error) *jwksRefresher {
	return &jwksRefresher{fetch: fetch, now: time.Now, jitter: equalJitter}
}

func (r *jwksRefresher) Refresh(ctx context.Context) error {
	return r.refresh(ctx, 0)
}

func (r *jwksRefresher) RefreshIfStale(ctx context.Context) error {
	return r.refresh(ctx, jwksUnknownKeyRefreshInterval)
}

func (r *jwksRefresher) refresh(ctx context.Context, minAge time.Duration) error {
	call, err := r.join(minAge)
	if err != nil {
		return err
	}
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		return fmt.Errorf("wait for JWKS refresh: %w", ctx.Err())
	}
}

func (r *jwksRefresher) join(minAge time.Duration) (*jwksRefreshCall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight != nil {
		return r.inflight, nil
	}
	if minAge > 0 && !r.lastSuccess.IsZero() && r.now().Sub(r.lastSuccess) < minAge {
		return nil, errJWKSRefreshRecent
	}
	if wait := r.notBefore.Sub(r.now()); wait > 0 {
		return nil, fmt.Errorf("%w (retry in %s): %w", errJWKSRefreshBackoff, wait.Round(time.Millisecond), r.lastErr)
	}
	r.inflight = &jwksRefreshCall{done: make(chan struct{})}
	go r.run(r.inflight, jwksFetchTimeout)
	return r.inflight, nil
}

func (r *jwksRefresher) run(call *jwksRefreshCall, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	call.err = r.fetch(ctx)
	r.settle(call)
	close(call.done)
}

func (r *jwksRefresher) settle(call *jwksRefreshCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflight = nil
	if call.err == nil {
		r.failures, r.notBefore, r.lastErr, r.lastSuccess = 0, time.Time{}, nil, r.now()
		return
	}
	r.failures++
	r.lastErr = call.err
	r.notBefore = r.now().Add(r.jitter(jwksRefreshBackoff(r.failures)))
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
		errJWKSStale, age.Round(time.Second), jwksStaleAfter, r.bgFailures)
	cause := r.bgErr
	if cause == nil {
		cause = r.lastErr
	}
	if cause != nil {
		err = fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

func jwksRefreshBackoff(failures int) time.Duration {
	d := jwksRefreshBackoffBase
	for i := 1; i < failures && d < jwksRefreshBackoffCap; i++ {
		d *= 2
	}
	return min(d, jwksRefreshBackoffCap)
}

func equalJitter(d time.Duration) time.Duration {
	half := d / 2
	return half + rand.N(half+1)
}

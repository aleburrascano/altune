package providers

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// jwksRefreshBackoffBase is the delay after the first failed forced refresh;
// each further consecutive failure doubles it up to jwksRefreshBackoffCap.
const jwksRefreshBackoffBase = time.Second

// jwksRefreshBackoffCap bounds how long a sustained JWKS outage defers the next
// forced refresh, so recovery is noticed within this window.
const jwksRefreshBackoffCap = 30 * time.Second

// jwksUnknownKeyRefreshInterval is the minimum time between a successful JWKS
// fetch and a refresh forced by an unknown signing key. A real rotation is
// picked up on the first token carrying the new kid, while a flood of tokens
// with made-up kids costs at most one fetch per interval.
const jwksUnknownKeyRefreshInterval = 30 * time.Second

// errJWKSRefreshRecent marks an unknown-key refresh refused because the key set
// was fetched successfully within jwksUnknownKeyRefreshInterval.
var errJWKSRefreshRecent = errors.New("JWKS refreshed recently")

// errJWKSRefreshBackoff marks a forced refresh refused because a recent attempt
// failed and the backoff window has not elapsed; the caller fails fast.
var errJWKSRefreshBackoff = errors.New("JWKS refresh backing off after failure")

// jwksRefresher coalesces forced JWKS fetches. At most one fetch is in flight at
// a time and every concurrent caller shares its result; after a failure further
// attempts are refused until a jittered, capped exponential backoff elapses; and
// each caller waits only as long as its own context allows. The shared fetch
// runs under its own jwksFetchTimeout context, so a canceled caller neither
// aborts it for the others nor stays parked behind it.
//
// It is the single choke point for forced refreshes: both the unprimed path
// (Refresh) and the unknown-kid path (RefreshIfStale) go through it, and it
// keeps the last-success bookkeeping that rate-limits the latter.
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
}

// jwksRefreshCall is one shared in-flight fetch; err is set before done closes.
type jwksRefreshCall struct {
	done chan struct{}
	err  error
}

func newJWKSRefresher(fetch func(context.Context) error) *jwksRefresher {
	return &jwksRefresher{fetch: fetch, now: time.Now, jitter: equalJitter}
}

// Refresh joins the in-flight fetch or starts one, and returns its error. It
// returns errJWKSRefreshBackoff (wrapping the last failure) without fetching
// while backing off, and ctx.Err() if the caller's context ends first.
func (r *jwksRefresher) Refresh(ctx context.Context) error {
	return r.refresh(ctx, 0)
}

// RefreshIfStale is Refresh for an unknown signing key: it also returns
// errJWKSRefreshRecent without fetching when the last successful fetch is
// younger than jwksUnknownKeyRefreshInterval, so unknown kids cannot force
// fetches faster than that. An in-flight fetch is always joined.
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

// join returns the in-flight fetch or starts one. A positive minAge refuses to
// start a fetch while the last success is younger than minAge.
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

// settle clears the in-flight slot and records the outcome for backoff.
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

// jwksRefreshBackoff returns the un-jittered delay after the given number of
// consecutive failures: base doubling per failure, clamped to the cap.
func jwksRefreshBackoff(failures int) time.Duration {
	d := jwksRefreshBackoffBase
	for i := 1; i < failures && d < jwksRefreshBackoffCap; i++ {
		d *= 2
	}
	return min(d, jwksRefreshBackoffCap)
}

// equalJitter returns a random duration in [d/2, d], keeping a floor so the
// backoff never collapses to zero while still desynchronizing instances.
func equalJitter(d time.Duration) time.Duration {
	half := d / 2
	return half + rand.N(half+1)
}

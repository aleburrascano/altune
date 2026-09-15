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

// jwksBackgroundRefreshInterval is how often the cache's background worker
// re-fetches the key set. It is pinned rather than derived from the endpoint's
// Cache-Control/Expires headers so jwksStaleAfter is a fixed multiple of the
// real cadence: a long advertised max-age would otherwise make a healthy, idle
// verifier look stale. It is a var only so tests can shorten it.
var jwksBackgroundRefreshInterval = 15 * time.Minute

// jwksRefreshWindow is how often the background worker checks for due
// refreshes, so a refresh fires at most this late. The library default (15
// minutes) would stretch the effective cadence to up to 30 minutes. It is a var
// only so tests can shorten it; it must not exceed jwksBackgroundRefreshInterval.
var jwksRefreshWindow = time.Minute

// jwksStaleAfter is how old the cached key set may get before CheckHealth
// reports auth degraded. Stale keys still verify tokens (the cache serves the
// last good set), so the bound only has to catch a sustained outage before a
// signing-key rotation turns it into rejected logins, while riding out
// transient blips without paging: with a background refresh every
// jwksBackgroundRefreshInterval (plus up to jwksRefreshWindow of lag), the key
// set crosses this bound only after three consecutive refreshes have failed,
// i.e. JWKS has been unreachable for about 45 minutes. A single success resets
// the age, and the age only grows while refreshes keep failing, so the
// dependency_down alert fires once per outage instead of flapping.
const jwksStaleAfter = time.Hour

// errJWKSStale marks a key set that is still being served but has not been
// refreshed successfully within jwksStaleAfter.
var errJWKSStale = errors.New("JWKS key set is stale")

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

	// keySetAt is when any fetch (startup, forced, or background) last stored a
	// valid key set; unlike lastSuccess, which only forced refreshes set and
	// which rate-limits unknown-key refreshes, it measures staleness.
	// bgFailures and bgErr describe background refreshes failing since then.
	keySetAt   time.Time
	bgFailures int
	bgErr      error
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

// recordKeySet notes that a fetch just stored a valid key set, resetting the
// staleness clock and the background failure streak.
func (r *jwksRefresher) recordKeySet() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keySetAt, r.bgFailures, r.bgErr = r.now(), 0, nil
}

// recordBackgroundFailure notes a failed background refresh and returns the
// failure streak and the age of the key set still being served (zero if none
// was ever fetched).
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

// checkFresh returns an errJWKSStale error once the last stored key set is
// older than jwksStaleAfter, naming its age, the background failure streak, and
// the latest failure. It returns nil while no key set was ever stored: that
// state is reported by the fetch path, not here.
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

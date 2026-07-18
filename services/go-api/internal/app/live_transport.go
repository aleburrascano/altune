package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// providerRateLimits caps requests/second per provider host on the LIVE path.
// Exceeding a provider's published limit is what got our IP throttled and
// surfaced as spurious search timeouts, so we pace to stay under it. Hosts not
// listed run unbounded (Deezer; SoundCloud carries its own client_id throttle).
var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":       1,   // ~1 req/sec, strict + documented
	"itunes.apple.com":      0.5, // ~20 req/min
	"ws.audioscrobbler.com": 5,   // Last.fm
	"music.youtube.com":     2,   // YouTube Music InnerTube
}

// liveTransport paces live provider calls under each host's rate limit and
// retries transient failures (timeouts mid-retry aside, 429 and 5xx) with a small
// backoff. It wraps the real network transport and is used for production and
// recording — NEVER replay, where the Replayer answers instantly from fixtures
// and pacing would only waste time.
type liveTransport struct {
	base     http.RoundTripper
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

// NewLiveTransport builds a rate-limiting, retrying transport over the default
// network transport. The recording eval wraps one of these so a bulk record paces
// itself instead of hammering providers into throttling.
func NewLiveTransport() http.RoundTripper {
	return &liveTransport{base: http.DefaultTransport, limiters: map[string]*rate.Limiter{}}
}

// limiter returns the per-host limiter, or nil for an unlisted (unbounded) host.
// nil is memoized so an unlisted host isn't re-checked on every request.
func (t *liveTransport) limiter(host string) *rate.Limiter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if l, ok := t.limiters[host]; ok {
		return l
	}
	var l *rate.Limiter
	if lim, ok := providerRateLimits[host]; ok {
		// Burst covers one query's per-kind calls (artist+track+album = 3, +margin)
		// so a single search isn't rate-limited against itself; the sustained rate
		// still paces throughput ACROSS queries, which is what providers throttle on.
		l = rate.NewLimiter(lim, 4)
	}
	t.limiters[host] = l
	return l
}

const liveMaxAttempts = 3

func (t *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < liveMaxAttempts; attempt++ {
		if attempt > 0 {
			body, err := rewindBody(req)
			if err != nil {
				return nil, err // body not replayable — cannot retry safely
			}
			req.Body = body
			if err := backoff(req.Context(), attempt); err != nil {
				return nil, err
			}
		}

		if l := t.limiter(req.URL.Host); l != nil {
			if err := l.Wait(req.Context()); err != nil {
				return nil, err
			}
		}

		resp, err := t.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			// A cancelled/expired context means the caller's budget is gone — more
			// attempts would only fail the same way.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}
		if attempt < liveMaxAttempts-1 && retryableStatus(resp.StatusCode) {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("upstream status %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// retryableStatus reports whether an HTTP status warrants a retry: rate-limit
// (429) and server errors (5xx). 4xx other than 429 are the caller's fault and
// won't change on retry.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// rewindBody returns a fresh copy of the request body for a retry, or nil when the
// request has no body. A body that cannot be rewound (no GetBody) is an error so
// the caller stops retrying rather than re-sending a consumed body.
func rewindBody(req *http.Request) (io.ReadCloser, error) {
	if req.Body == nil {
		return nil, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("live transport: request body is not replayable for retry")
	}
	return req.GetBody()
}

// backoff waits a small linear delay before a retry, returning early if the
// caller's context is cancelled or its deadline passes.
func backoff(ctx context.Context, attempt int) error {
	t := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

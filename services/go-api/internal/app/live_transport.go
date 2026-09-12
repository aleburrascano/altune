package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":       1,
	"itunes.apple.com":      0.5,
	"ws.audioscrobbler.com": 5,
	"music.youtube.com":     2,
	"api.discogs.com":       1,
}

type liveTransport struct {
	base     http.RoundTripper
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	// sleep is a test seam; when nil a real timer is used.
	sleep func(context.Context, time.Duration) error
}

func NewLiveTransport() http.RoundTripper {
	return &liveTransport{base: http.DefaultTransport, limiters: map[string]*rate.Limiter{}}
}

func (t *liveTransport) limiter(host string) *rate.Limiter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if l, ok := t.limiters[host]; ok {
		return l
	}
	var l *rate.Limiter
	if lim, ok := providerRateLimits[host]; ok {
		l = rate.NewLimiter(lim, 4)
	}
	t.limiters[host] = l
	return l
}

const liveMaxAttempts = 3

func (t *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt < liveMaxAttempts; attempt++ {
		if attempt > 0 {
			body, err := rewindBody(req)
			if err != nil {
				return nil, err
			}
			req.Body = body
			if err := t.wait(req.Context(), retryDelay(attempt, retryAfter)); err != nil {
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
			retryAfter = 0
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}
		if attempt < liveMaxAttempts-1 && retryableStatus(resp.StatusCode) {
			retryAfter, _ = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("upstream status %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func rewindBody(req *http.Request) (io.ReadCloser, error) {
	if req.Body == nil {
		return nil, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("live transport: request body is not replayable for retry")
	}
	return req.GetBody()
}

// liveMaxRetryAfter caps an honored Retry-After so a malicious or huge value
// cannot wedge the client.
const liveMaxRetryAfter = 30 * time.Second

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return fixedBackoff(attempt)
}

func fixedBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 250 * time.Millisecond
}

// parseRetryAfter reads a Retry-After value in either supported form —
// delta-seconds or an HTTP-date — returning the capped delay when valid.
func parseRetryAfter(h string, now time.Time) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0, false
		}
		return capRetryAfter(time.Duration(secs) * time.Second), true
	}
	when, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	return capRetryAfter(when.Sub(now)), true
}

func capRetryAfter(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > liveMaxRetryAfter {
		return liveMaxRetryAfter
	}
	return d
}

func (t *liveTransport) wait(ctx context.Context, d time.Duration) error {
	if t.sleep != nil {
		return t.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

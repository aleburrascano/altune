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
	for attempt := 0; attempt < liveMaxAttempts; attempt++ {
		if attempt > 0 {
			body, err := rewindBody(req)
			if err != nil {
				return nil, err
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

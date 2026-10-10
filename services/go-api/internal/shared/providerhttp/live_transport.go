package providerhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":            1,
	"itunes.apple.com":           0.5,
	"ws.audioscrobbler.com":      5,
	"music.youtube.com":          2,
	"api.discogs.com":            1,
	"api.deezer.com":             5,
	"api-v2.soundcloud.com":      3,
	"na.web.skill.music.a2z.com": 2,
	"api-partner.spotify.com":    3,
	"api.genius.com":             2,
	"webservice.fanart.tv":       2,
	"coverartarchive.org":        2,
	"api.music.apple.com":        3,
	"open.spotify.com":           3,
	"auth.deezer.com":            1,
	"pipe.deezer.com":            3,
	"theaudiodb.com":             0.5,
	"music.amazon.com":           2,
	"music.apple.com":            2,
	"clienttoken.spotify.com":    2,
	"soundcloud.com":             2,
}

const defaultProviderBurst = 4

var providerBursts = map[string]int{
	"musicbrainz.org": 1,
}

type liveTransport struct {
	base      http.RoundTripper
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
	sleep     func(context.Context, time.Duration) error
	randFloat func() float64
	onRetry   func(host string)
}

type LiveOption func(*liveTransport)

func WithRetryRecorder(record func(host string)) LiveOption {
	return func(t *liveTransport) { t.onRetry = record }
}

func NewLiveTransport(opts ...LiveOption) http.RoundTripper {
	t := &liveTransport{base: baseTransport(), limiters: map[string]*rate.Limiter{}}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *liveTransport) limiter(host string) *rate.Limiter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if l, ok := t.limiters[host]; ok {
		return l
	}
	var l *rate.Limiter
	if lim, ok := providerRateLimits[host]; ok {
		l = rate.NewLimiter(lim, providerBurst(host))
	}
	t.limiters[host] = l
	return l
}

func providerBurst(host string) int {
	if b, ok := providerBursts[host]; ok {
		return b
	}
	return defaultProviderBurst
}

const liveMaxAttempts = 3

func (t *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var last attemptResult
	for attempt := 0; attempt < liveMaxAttempts; attempt++ {
		if attempt > 0 {
			if err := t.prepareRetry(req, attempt, last); err != nil {
				return nil, err
			}
		}
		resp, result, done := t.attempt(req, attempt)
		if done {
			return resp, result.err
		}
		last = result
	}
	return nil, last.err
}

type attemptResult struct {
	err        error
	retryAfter time.Duration
	cause      retryCause
}

func (t *liveTransport) attempt(req *http.Request, attempt int) (*http.Response, attemptResult, bool) {
	if err := t.awaitRateLimit(req); err != nil {
		return nil, attemptResult{err: err}, true
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, attemptResult{err: err}, true
		}
		return nil, attemptResult{err: err, cause: retryCause{errorClass: transportErrorClass(err)}}, false
	}
	if attempt < liveMaxAttempts-1 && retryableStatus(resp.StatusCode) {
		retryAfter, _ := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		_ = resp.Body.Close()
		failed := fmt.Errorf("upstream status %d", resp.StatusCode)
		return nil, attemptResult{err: failed, retryAfter: retryAfter, cause: retryCause{status: resp.StatusCode}}, false
	}
	return resp, attemptResult{}, true
}

type retryCause struct {
	status     int
	errorClass string
}

func (c retryCause) attr() slog.Attr {
	if c.status != 0 {
		return slog.Int("status", c.status)
	}
	return slog.String("error_class", c.errorClass)
}

func transportErrorClass(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op
	}
	return "transport"
}

func (t *liveTransport) prepareRetry(req *http.Request, attempt int, last attemptResult) error {
	body, err := rewindBody(req)
	if err != nil {
		return err
	}
	req.Body = body
	delay := t.retryDelay(attempt, last.retryAfter)
	t.reportRetry(req, attempt, last.cause, delay)
	return t.wait(req.Context(), delay)
}

func (t *liveTransport) reportRetry(req *http.Request, attempt int, cause retryCause, delay time.Duration) {
	host := req.URL.Hostname()
	slog.WarnContext(req.Context(), "provider.retry",
		"host", host, "attempt", attempt+1, cause.attr(), "delay", delay)
	if t.onRetry != nil {
		t.onRetry(host)
	}
}

func (t *liveTransport) awaitRateLimit(req *http.Request) error {
	l := t.limiter(req.URL.Host)
	if l == nil {
		return nil
	}
	return l.Wait(req.Context())
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func rewindBody(req *http.Request) (io.ReadCloser, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req.Body, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("live transport: request body is not replayable for retry")
	}
	return req.GetBody()
}

const liveMaxRetryAfter = 30 * time.Second

func (t *liveTransport) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return t.fixedBackoff(attempt)
}

const liveBackoffBase = 250 * time.Millisecond

const liveBackoffJitter = 0.2

const liveMaxBackoff = liveMaxAttempts * liveBackoffBase

func (t *liveTransport) fixedBackoff(attempt int) time.Duration {
	base := time.Duration(attempt) * liveBackoffBase
	if base <= 0 {
		return 0
	}
	next := rand.Float64
	if t.randFloat != nil {
		next = t.randFloat
	}
	delta := (next()*2 - 1) * liveBackoffJitter * float64(base)
	d := base + time.Duration(delta)
	if d < 0 {
		d = 0
	}
	if d > liveMaxBackoff {
		d = liveMaxBackoff
	}
	return d
}

func parseRetryAfter(h string, now time.Time) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(h); err == nil {
		return retryAfterFromSeconds(secs)
	}
	when, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	return capRetryAfter(when.Sub(now)), true
}

const liveMaxRetryAfterSeconds = int(liveMaxRetryAfter / time.Second)

func retryAfterFromSeconds(secs int) (time.Duration, bool) {
	if secs < 0 {
		return 0, false
	}
	if secs > liveMaxRetryAfterSeconds {
		return liveMaxRetryAfter, true
	}
	return time.Duration(secs) * time.Second, true
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

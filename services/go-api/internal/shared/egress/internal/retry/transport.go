package retry

import (
	"altune/go-api/internal/shared/egress/internal/pacing"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

const maxAttempts = 3

type Options struct {
	Sleep   func(context.Context, time.Duration) error
	Jitter  func() float64
	OnRetry func(host string)
}

type Transport struct {
	base    http.RoundTripper
	pacer   *pacing.Pacer
	sleep   func(context.Context, time.Duration) error
	jitter  func() float64
	onRetry func(host string)
}

func New(base http.RoundTripper, opts Options) *Transport {
	t := &Transport{base: base, pacer: pacing.New(), sleep: opts.Sleep, jitter: opts.Jitter, onRetry: opts.OnRetry}
	if t.sleep == nil {
		t.sleep = sleepOnTimer
	}
	if t.jitter == nil {
		t.jitter = rand.Float64
	}
	if t.onRetry == nil {
		t.onRetry = func(string) {}
	}
	return t
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var last attemptResult
	for attempt := 0; attempt < maxAttempts; attempt++ {
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

func (t *Transport) attempt(req *http.Request, attempt int) (*http.Response, attemptResult, bool) {
	if err := t.pacer.Wait(req); err != nil {
		return nil, attemptResult{err: err}, true
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, attemptResult{err: err}, true
		}
		return nil, attemptResult{err: err, cause: retryCause{errorClass: transportErrorClass(err)}}, false
	}
	if attempt < maxAttempts-1 && retryableStatus(resp.StatusCode) {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
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

func (t *Transport) prepareRetry(req *http.Request, attempt int, last attemptResult) error {
	body, err := rewindBody(req)
	if err != nil {
		return err
	}
	req.Body = body
	delay := t.retryDelay(attempt, last.retryAfter)
	t.reportRetry(req, attempt, last.cause, delay)
	return t.sleep(req.Context(), delay)
}

func (t *Transport) reportRetry(req *http.Request, attempt int, cause retryCause, delay time.Duration) {
	host := req.URL.Hostname()
	slog.WarnContext(req.Context(), "provider.retry",
		"host", host, "attempt", attempt+1, cause.attr(), "delay", delay)
	t.onRetry(host)
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

func (t *Transport) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return t.backoff(attempt)
}

const (
	backoffBaseMillis = 250
	backoffJitter     = 0.2
)

func (t *Transport) backoff(attempt int) time.Duration {
	base := time.Duration(attempt*backoffBaseMillis) * time.Millisecond
	delta := (t.jitter()*2 - 1) * backoffJitter * float64(base)
	return base + time.Duration(delta)
}

func sleepOnTimer(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package retry

import (
	"altune/go-api/internal/platform/outbound/internal/adapters/pacing"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const maxAttempts = 3

type Options struct {
	Now     clock.Func
	Sleep   func(context.Context, time.Duration) error
	Jitter  func() float64
	OnRetry func(host string)
}

type Transport struct {
	base    http.RoundTripper
	pacer   *pacing.Pacer
	now     clock.Func
	sleep   func(context.Context, time.Duration) error
	jitter  func() float64
	onRetry func(host string)
}

func New(base http.RoundTripper, pacer *pacing.Pacer, opts Options) (*Transport, error) {
	if pacer == nil {
		return nil, ErrPacerRequired
	}
	if opts.Now == nil || opts.Sleep == nil || opts.Jitter == nil || opts.OnRetry == nil {
		return nil, ErrOptionsRequired
	}
	return &Transport{base: base, pacer: pacer, now: opts.Now, sleep: opts.Sleep, jitter: opts.Jitter, onRetry: opts.OnRetry}, nil
}

var (
	ErrPacerRequired   = errors.New("retry transport needs a pacer")
	ErrOptionsRequired = errors.New("retry transport needs its clock, sleep, jitter and retry recorder, start from SystemOptions")
)

func SystemOptions() Options {
	return Options{Now: clock.Now, Sleep: clock.Sleep, Jitter: random.Real, OnRetry: func(string) {}}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	attempts := 1
	if mayRetry(req) {
		attempts = maxAttempts
	}
	var last attemptResult
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := t.prepareRetry(req, attempt, last); err != nil {
				return nil, err
			}
		}
		resp, result, done := t.attempt(req, attempt, attempts)
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

func (t *Transport) attempt(req *http.Request, attempt, attempts int) (*http.Response, attemptResult, bool) {
	pace := t.pacer.Wait
	if attempt > 0 {
		pace = t.pacer.WaitRetry
	}
	if err := pace(req); err != nil {
		return nil, attemptResult{err: err}, true
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, attemptResult{err: err}, true
		}
		return nil, attemptResult{err: err, cause: retryCause{errorClass: transportErrorClass(err)}}, false
	}
	if attempt < attempts-1 && retryableStatus(resp.StatusCode) {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), t.now())
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
		return slog.Int(logging.KeyStatus, c.status)
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
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("rewind the request body: %w", err)
	}
	return body, nil
}

func (t *Transport) retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return t.backoff(attempt)
}

const (
	backoffBase   = 250 * time.Millisecond
	backoffSpread = 0.2
)

var retryBackoff = backoff.MustCapped(backoffBase, backoffBase<<(maxAttempts-2), backoff.WithSpread(backoffSpread))

func (t *Transport) backoff(attempt int) time.Duration {
	return retryBackoff.DelayAt(attempt, t.jitter())
}

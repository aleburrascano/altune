package providers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	codeUnauthorized = "tracker_unauthorized"
	codeRateLimited  = "tracker_rate_limited"
	codeRejected     = "tracker_rejected"
	codeNotFound     = "tracker_not_found"
	codeUnavailable  = "tracker_unavailable"
	codeUnreachable  = "tracker_unreachable"

	codeOutcomeUnknown = "tracker_outcome_unknown"
)

type trackerError struct {
	status  int
	code    string
	backoff time.Duration
	err     error
}

func (e *trackerError) Error() string     { return e.err.Error() }
func (e *trackerError) Unwrap() error     { return e.err }
func (e *trackerError) HTTPStatus() int   { return e.status }
func (e *trackerError) ErrorCode() string { return e.code }

func (e *trackerError) ClientDetail() string {
	switch e.code {
	case codeUnauthorized:
		return "issue tracker refused access"
	case codeRateLimited:
		return "issue tracker is rate limited"
	case codeRejected:
		return "issue tracker rejected the report"
	case codeNotFound:
		return "issue tracker is misconfigured"
	case codeUnreachable:
		return "issue tracker unreachable"
	default:
		return "issue tracker unavailable"
	}
}

func (e *trackerError) Throttled() (time.Duration, bool) {
	return e.backoff, e.code == codeRateLimited
}

func (e *trackerError) RetryAfter() time.Duration {
	if backoff, ok := e.Throttled(); ok {
		return min(backoff, maxForwardedRetryAfter)
	}
	return 0
}

func (e *trackerError) Uncreated() bool { return true }

type outcomeUnknownError struct{ err error }

func (e *outcomeUnknownError) Error() string     { return e.err.Error() }
func (e *outcomeUnknownError) Unwrap() error     { return e.err }
func (e *outcomeUnknownError) HTTPStatus() int   { return http.StatusBadGateway }
func (e *outcomeUnknownError) ErrorCode() string { return codeOutcomeUnknown }
func (e *outcomeUnknownError) ClientDetail() string {
	return "issue tracker did not confirm the report"
}
func (e *outcomeUnknownError) Uncreated() bool { return false }

func outcomeUnknown(err error) error {
	return &outcomeUnknownError{err: err}
}

func transportError(err error) error {
	if wasNeverSent(err) {
		return &trackerError{status: http.StatusGatewayTimeout, code: codeUnreachable, err: wrapErr(err)}
	}
	return outcomeUnknown(wrapErr(err))
}

func wasNeverSent(err error) bool {
	var unresolved *net.DNSError
	if errors.As(err, &unresolved) {
		return true
	}
	var refused *net.OpError
	return errors.As(err, &refused) && refused.Op == "dial"
}

func statusError(resp *http.Response, now time.Time) error {
	body := readErrorBody(resp)
	status, code := classify(resp, body)
	return &trackerError{
		status:  status,
		code:    code,
		backoff: requestedBackoff(resp.Header, now),
		err:     wrapErr(fmt.Errorf("status %d: %s", resp.StatusCode, body)),
	}
}

func classify(resp *http.Response, body string) (int, string) {
	switch {
	case isRateLimited(resp, body):
		return http.StatusServiceUnavailable, codeRateLimited
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return http.StatusBadGateway, codeUnauthorized
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return http.StatusBadGateway, codeRejected
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return http.StatusBadGateway, codeNotFound
	default:
		return http.StatusBadGateway, codeUnavailable
	}
}

func isRateLimited(resp *http.Response, body string) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return resp.Header.Get("Retry-After") != "" ||
		resp.Header.Get("X-RateLimit-Remaining") == "0" ||
		strings.Contains(strings.ToLower(body), "rate limit")
}

func requestedBackoff(h http.Header, now time.Time) time.Duration {
	if secs, err := strconv.ParseInt(h.Get("Retry-After"), 10, 64); err == nil && secs > 0 {
		return time.Duration(min(secs, maxRequestedBackoffSecs)) * time.Second
	}
	if h.Get("X-RateLimit-Remaining") != "0" {
		return 0
	}
	reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return 0
	}
	if reset <= now.Unix() {
		return 0
	}
	if reset-now.Unix() > maxRequestedBackoffSecs {
		return maxRequestedBackoffSecs * time.Second
	}
	return time.Unix(reset, 0).Sub(now)
}

const maxRequestedBackoffSecs = 24 * 60 * 60

const maxForwardedRetryAfter = time.Hour

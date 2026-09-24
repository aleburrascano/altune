package providers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Wire codes let a caller tell one GitHub failure from another even when two
// map to the same HTTP status. They are stable and namespaced to this adapter.
const (
	codeUnauthorized = "tracker_unauthorized"
	codeRateLimited  = "tracker_rate_limited"
	codeRejected     = "tracker_rejected"
	codeNotFound     = "tracker_not_found"
	codeUnavailable  = "tracker_unavailable"
	codeUnreachable  = "tracker_unreachable"
)

// trackerError classifies a GitHub issue-creation failure so callers surface an
// auth problem, a rate limit, or an outage as distinct statuses/codes instead
// of one generic 500. It implements httputil's StatusError and ErrorCoder,
// ports.TrackerThrottle so the application can back off a rate-limited token,
// and ports.TrackerUncreated so it can release the quota of a failed attempt.
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

// Throttled reports whether GitHub refused the call as rate limited, and for how
// long it asked callers to wait.
func (e *trackerError) Throttled() (time.Duration, bool) {
	return e.backoff, e.code == codeRateLimited
}

// Uncreated reports that no issue exists: a trackerError is only built for a
// non-201 answer or a transport failure. A 201 whose body cannot be decoded is
// deliberately a plain error, since GitHub already created that issue.
func (e *trackerError) Uncreated() bool { return true }

// networkError classifies a transport-level failure (dial, timeout, reset): the
// tracker never answered, so it reads as an unreachable upstream.
func networkError(err error) error {
	return &trackerError{status: http.StatusGatewayTimeout, code: codeUnreachable, err: wrapErr(err)}
}

// statusError classifies a non-201 GitHub response and captures Retry-After and
// the requested backoff so a rate-limited caller knows when to come back.
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

// classify maps a GitHub status onto the status and code our API surfaces.
func classify(resp *http.Response, body string) (int, string) {
	switch {
	case isRateLimited(resp, body):
		return http.StatusServiceUnavailable, codeRateLimited
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return http.StatusBadGateway, codeUnauthorized
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return http.StatusBadGateway, codeRejected
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// A wrong repo, a token that cannot see it, or issues disabled: permanent
		// misconfiguration, kept apart from a transient outage.
		return http.StatusBadGateway, codeNotFound
	default:
		return http.StatusBadGateway, codeUnavailable
	}
}

// isRateLimited spots GitHub's rate-limit shapes: a plain 429, or a 403 that
// carries a Retry-After, an exhausted X-RateLimit-Remaining, or (for a secondary
// limit sent without either header) a body naming the rate limit.
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

// requestedBackoff reads how long GitHub asked us to wait, per its documented
// order: Retry-After seconds first, else the X-RateLimit-Reset epoch when the
// remaining quota is exhausted. Zero means GitHub gave no usable hint. Seconds
// are clamped before converting so an absurd header cannot overflow into a
// tiny wait.
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

// maxRequestedBackoffSecs bounds a GitHub wait hint to one day; the application
// applies its own, tighter ceiling on top.
const maxRequestedBackoffSecs = 24 * 60 * 60

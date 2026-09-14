package providers

import (
	"fmt"
	"net/http"
)

// Wire codes let a caller tell one GitHub failure from another even when two
// map to the same HTTP status. They are stable and namespaced to this adapter.
const (
	codeUnauthorized = "tracker_unauthorized"
	codeRateLimited  = "tracker_rate_limited"
	codeRejected     = "tracker_rejected"
	codeUnavailable  = "tracker_unavailable"
	codeUnreachable  = "tracker_unreachable"
)

// trackerError classifies a GitHub issue-creation failure so callers surface an
// auth problem, a rate limit, or an outage as distinct statuses/codes instead
// of one generic 500. It implements httputil's StatusError and ErrorCoder.
type trackerError struct {
	status     int    // HTTP status this failure should surface to our caller
	code       string // stable wire code, distinct even when statuses collide
	retryAfter string // GitHub's Retry-After header verbatim, "" when absent
	err        error  // wrapped cause carrying the human-readable message
}

func (e *trackerError) Error() string      { return e.err.Error() }
func (e *trackerError) Unwrap() error      { return e.err }
func (e *trackerError) HTTPStatus() int    { return e.status }
func (e *trackerError) ErrorCode() string  { return e.code }
func (e *trackerError) RetryAfter() string { return e.retryAfter }

// networkError classifies a transport-level failure (dial, timeout, reset): the
// tracker never answered, so it reads as an unreachable upstream.
func networkError(err error) error {
	return &trackerError{status: http.StatusGatewayTimeout, code: codeUnreachable, err: wrapErr(err)}
}

// statusError classifies a non-201 GitHub response and captures Retry-After so
// a rate-limited caller can be told when to come back.
func statusError(resp *http.Response) error {
	status, code := classify(resp)
	return &trackerError{
		status:     status,
		code:       code,
		retryAfter: resp.Header.Get("Retry-After"),
		err:        wrapErr(fmt.Errorf("status %d: %s", resp.StatusCode, readErrorBody(resp))),
	}
}

// classify maps a GitHub status onto the status and code our API surfaces.
func classify(resp *http.Response) (int, string) {
	switch {
	case isRateLimited(resp):
		return http.StatusServiceUnavailable, codeRateLimited
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return http.StatusBadGateway, codeUnauthorized
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return http.StatusBadGateway, codeRejected
	default:
		return http.StatusBadGateway, codeUnavailable
	}
}

// isRateLimited spots GitHub's two rate-limit shapes: a plain 429, or a 403 that
// carries a Retry-After or an exhausted X-RateLimit-Remaining.
func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"
}

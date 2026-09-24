package goapi

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNoToken is returned by a TokenSource that has no read-only token to present.
// The client fails closed on it: no request leaves without credentials.
var ErrNoToken = errors.New("goapi: no read-only token available")

// SourceDownError reports that go-api could not be reached at all — connection
// refused, DNS failure, TLS handshake failure or a timeout before any response.
// It is distinct from APIError (go-api answered, just not with 2xx): the SSE
// leaf and buckets branch on it to serve last-known state flagged stale plus an
// explicit "source down" signal (the outlives-the-app invariant). Match it with
// errors.As or the IsSourceDown helper.
type SourceDownError struct {
	// Op names the client operation that failed, for diagnostics.
	Op string
	// Err is the underlying transport error.
	Err error
	// CorrID is the correlation id the failed request carried, tying this
	// failure to go-api's own logs even though no response came back.
	CorrID string
}

func (e *SourceDownError) Error() string {
	return fmt.Sprintf("goapi: source down during %s: %v%s", e.Op, e.Err, corrSuffix(e.CorrID))
}

// Unwrap exposes the transport error to errors.Is/As.
func (e *SourceDownError) Unwrap() error { return e.Err }

// IsSourceDown reports whether err is (or wraps) a SourceDownError, i.e. go-api
// was unreachable. Callers use it to decide between "app is down" and "app said
// no".
func IsSourceDown(err error) bool {
	var sd *SourceDownError
	return errors.As(err, &sd)
}

// APIError reports that go-api answered with a non-2xx status. Unlike
// SourceDownError, the app is reachable — this is a request-level failure such
// as 401 (token rejected) or 404. Body holds a bounded snippet of the response
// for diagnostics.
type APIError struct {
	// Op names the client operation that failed.
	Op string
	// StatusCode is the HTTP status go-api returned.
	StatusCode int
	// CorrID is the correlation id go-api echoed on the response, tying this
	// failure to the matching go-api log line.
	CorrID string
	// Body is a bounded snippet of the response body, for diagnostics.
	Body string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("goapi: %s: unexpected status %d: %s%s", e.Op, e.StatusCode, e.Body, corrSuffix(e.CorrID))
}

// TokenError reports that acquiring the read-only bearer token failed before a
// request could even be built — no credentials, no request sent. It is the
// "auth" reason's other source besides a 401/403 APIError: a dead refresh chain
// fails here, never reaching go-api at all, and must still classify as our
// credential's fault rather than go-api being down.
type TokenError struct {
	// Op names the client operation that needed the token.
	Op string
	// Err is the underlying TokenSource failure.
	Err error
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("goapi: %s: %v", e.Op, e.Err)
}

// Unwrap exposes the TokenSource failure to errors.Is/As.
func (e *TokenError) Unwrap() error { return e.Err }

// Classify sorts a read failure into one of the four reasons a snapshot carries
// instead of one opaque "stale"/"source_down": "auth" (our credential — a
// TokenError, or go-api rejecting the token with 401/403), "throttled" (429,
// back off), "degraded" (503, go-api answered but a dependency is down),
// "down" (transport failure, timeout, or any other 5xx go-api could not
// explain). nil, or an error Classify does not recognise (a decode failure, a
// 4xx that is not 401/403/429), returns "": the caller's own state derivation
// still stands, this only adds the why when one is known.
func Classify(err error) string {
	if err == nil {
		return ""
	}
	var tokenErr *TokenError
	if errors.As(err, &tokenErr) {
		return "auth"
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "auth"
		case http.StatusTooManyRequests:
			return "throttled"
		case http.StatusServiceUnavailable:
			return "degraded"
		default:
			if apiErr.StatusCode >= 500 {
				return "down"
			}
			return ""
		}
	}
	if IsSourceDown(err) {
		return "down"
	}
	return ""
}

// corrSuffix renders a correlation id for an error message, or nothing when none
// was captured, so a failure with an id is greppable and one without stays clean.
func corrSuffix(id string) string {
	if id == "" {
		return ""
	}
	return " (corr_id=" + id + ")"
}

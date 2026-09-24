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

type TokenError struct {
	Op  string
	Err error
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("goapi: %s: %v", e.Op, e.Err)
}

func (e *TokenError) Unwrap() error { return e.Err }

const (
	ReasonAuth      = "auth"
	ReasonThrottled = "throttled"
	ReasonDegraded  = "degraded"
	ReasonDown      = "down"
)

func Classify(err error) string {
	if err == nil {
		return ""
	}
	var tokenErr *TokenError
	if errors.As(err, &tokenErr) {
		return ReasonAuth
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ReasonAuth
		case http.StatusTooManyRequests:
			return ReasonThrottled
		case http.StatusServiceUnavailable:
			return ReasonDegraded
		default:
			if apiErr.StatusCode >= 500 {
				return ReasonDown
			}
			return ""
		}
	}
	if IsSourceDown(err) {
		return ReasonDown
	}
	return ""
}

type errorSource interface {
	LastError() error
}

func StreamReason(src any) string {
	es, ok := src.(errorSource)
	if !ok {
		return ""
	}
	return Classify(es.LastError())
}

type statusSource interface {
	Status() Status
}

type healthSource interface {
	Health() (Status, error)
}

func StreamStatus(src statusSource) (Status, string) {
	if hs, ok := src.(healthSource); ok {
		status, err := hs.Health()
		return status, Classify(err)
	}
	return src.Status(), StreamReason(src)
}

// corrSuffix renders a correlation id for an error message, or nothing when none
// was captured, so a failure with an id is greppable and one without stays clean.
func corrSuffix(id string) string {
	if id == "" {
		return ""
	}
	return " (corr_id=" + id + ")"
}

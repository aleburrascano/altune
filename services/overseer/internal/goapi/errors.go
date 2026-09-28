package goapi

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrNoToken = errors.New("goapi: no read-only token available")

type SourceDownError struct {
	Op     string
	Err    error
	CorrID string
}

func (e *SourceDownError) Error() string {
	return fmt.Sprintf("goapi: source down during %s: %v%s", e.Op, e.Err, corrSuffix(e.CorrID))
}

func (e *SourceDownError) Unwrap() error { return e.Err }

func IsSourceDown(err error) bool {
	var sd *SourceDownError
	return errors.As(err, &sd)
}

type APIError struct {
	Op         string
	StatusCode int
	CorrID     string
	Body       string
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

func corrSuffix(id string) string {
	if id == "" {
		return ""
	}
	return " (corr_id=" + id + ")"
}

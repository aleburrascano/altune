package goapi

import "errors"

const (
	statusUnauthorized       = 401
	statusForbidden          = 403
	statusTooManyRequests    = 429
	statusServiceUnavailable = 503
)

func IsSourceDown(err error) bool {
	var sd *SourceDownError
	return errors.As(err, &sd)
}

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
		case statusUnauthorized, statusForbidden:
			return ReasonAuth
		case statusTooManyRequests:
			return ReasonThrottled
		case statusServiceUnavailable:
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

func streamReason(src any) string {
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
	return src.Status(), streamReason(src)
}

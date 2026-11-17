package core

import (
	"errors"
	"fmt"
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

func corrSuffix(id string) string {
	if id == "" {
		return ""
	}
	return " (corr_id=" + id + ")"
}

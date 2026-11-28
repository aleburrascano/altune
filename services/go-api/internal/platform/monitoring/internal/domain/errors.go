package domain

import "errors"

var ErrClientEventsUnavailable = errors.New("client event store unavailable")

type InvalidClientEventError struct {
	Reason string
}

func (e *InvalidClientEventError) Error() string {
	return e.Reason
}

func NewInvalidClientEventError(reason string) *InvalidClientEventError {
	return &InvalidClientEventError{Reason: reason}
}

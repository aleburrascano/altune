package domain

import "altune/go-api/internal/shared"

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409, Code: "catalog.track_already_in_playlist"}

// ValidationError aliases the shared type so this package keeps one name for
// its 400s while the implementation lives in internal/shared.
type ValidationError = shared.ValidationError

// NewValidationError builds a catalog validation error
// ("catalog.validation_error").
func NewValidationError(msg string) *ValidationError {
	return shared.NewValidationError("catalog", msg)
}

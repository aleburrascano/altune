package domain

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409, Code: "catalog.track_already_in_playlist"}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string     { return e.Message }
func (e *ValidationError) HTTPStatus() int   { return 400 }
func (e *ValidationError) ErrorCode() string { return "catalog.validation_error" }

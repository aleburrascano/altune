package domain

type CodedError struct {
	Msg    string
	Status int
}

func (e *CodedError) Error() string   { return e.Msg }
func (e *CodedError) HTTPStatus() int { return e.Status }

var ErrTrackAlreadyInPlaylist = &CodedError{Msg: "track already in playlist", Status: 409}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string   { return e.Message }
func (e *ValidationError) HTTPStatus() int { return 400 }

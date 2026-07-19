package domain

// CodedError is a domain/service error that carries its own HTTP status and a
// client-safe message. httputil.HandleServiceError maps it without the adapter
// re-deciding the status per handler. The status is a plain int so the domain
// layer stays free of net/http.
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

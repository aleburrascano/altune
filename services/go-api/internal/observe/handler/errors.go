package handler

import "net/http"

type codedError struct {
	msg    string
	status int
	code   string
}

func (e *codedError) Error() string     { return e.msg }
func (e *codedError) HTTPStatus() int   { return e.status }
func (e *codedError) ErrorCode() string { return e.code }

var errPrincipalRequired = &codedError{
	msg:    "overseer principal required",
	status: http.StatusForbidden,
	code:   "observe.principal_required",
}

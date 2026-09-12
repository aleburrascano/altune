package handler

import "net/http"

// codedError carries a stable, machine-checkable error code alongside its HTTP
// status and message. It implements httputil.StatusError and httputil.ErrorCoder
// so admin responses routed through httputil.HandleServiceError gain a `code`
// field, matching the metrics handler.
type codedError struct {
	msg    string
	status int
	code   string
}

func (e *codedError) Error() string     { return e.msg }
func (e *codedError) HTTPStatus() int   { return e.status }
func (e *codedError) ErrorCode() string { return e.code }

var (
	errReRunUnavailable = &codedError{
		msg:    "re-run inspector not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.rerun_inspector_unavailable",
	}
	errSearchUnavailable = &codedError{
		msg:    "test search not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.search_inspector_unavailable",
	}
	errDetailUnavailable = &codedError{
		msg:    "detail re-run inspector not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.detail_inspector_unavailable",
	}
	errRequestNotFound = &codedError{
		msg:    "request not found",
		status: http.StatusNotFound,
		code:   "admin.request_not_found",
	}
)

// upstreamError wraps an inspector failure as a 502 with a stable code while
// preserving the underlying message wording.
func upstreamError(code string, err error) *codedError {
	return &codedError{msg: err.Error(), status: http.StatusBadGateway, code: code}
}

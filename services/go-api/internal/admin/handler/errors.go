package handler

import (
	"errors"
	"net/http"
)

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
	errStreamSubscriberLimit = &codedError{
		msg:    "too many admin streams open",
		status: http.StatusTooManyRequests,
		code:   "admin.stream_subscriber_limit",
	}
	errOperatorRequired = &codedError{
		msg:    "operator access required",
		status: http.StatusForbidden,
		code:   "admin.operator_required",
	}
	errReadOnlyForbidden = &codedError{
		msg:    "read-only admin principal cannot use this route",
		status: http.StatusForbidden,
		code:   "admin.read_only_forbidden",
	}
	errMetricRequired = &codedError{
		msg:    "metric query param is required",
		status: http.StatusBadRequest,
		code:   "admin.metric_required",
	}
	errQueryRequired = &codedError{
		msg:    "query is required",
		status: http.StatusBadRequest,
		code:   "admin.query_required",
	}
	errRequestNotFound = &codedError{
		msg:    "request not found",
		status: http.StatusNotFound,
		code:   "admin.request_not_found",
	}
)

// ErrInspectorInvalidInput marks a rerun/test-search/rerun-detail failure caused
// by the caller's input (unknown kinds, empty or oversized query). The inspector
// wiring wraps such errors with it so the handler answers 400, not 502.
var ErrInspectorInvalidInput = errors.New("invalid inspector request")

// ErrInspectorProvidersDown marks an inspector failure caused by every discovery
// provider failing, so an outage carries its own 502 code distinct from both a
// bad request and any other upstream failure.
var ErrInspectorProvidersDown = errors.New("all providers failed")

// inspectorError maps an inspector failure to its coded response: 400
// admin.invalid_request for bad input, 502 admin.all_providers_failed for a
// total provider outage, and otherwise a 502 with the endpoint's failCode. The
// underlying message wording is preserved in every case.
func inspectorError(failCode string, err error) *codedError {
	switch {
	case errors.Is(err, ErrInspectorInvalidInput):
		return &codedError{msg: err.Error(), status: http.StatusBadRequest, code: "admin.invalid_request"}
	case errors.Is(err, ErrInspectorProvidersDown):
		return &codedError{msg: err.Error(), status: http.StatusBadGateway, code: "admin.all_providers_failed"}
	default:
		return upstreamError(failCode, err)
	}
}

// upstreamError wraps an inspector failure as a 502 with a stable code while
// preserving the underlying message wording.
func upstreamError(code string, err error) *codedError {
	return &codedError{msg: err.Error(), status: http.StatusBadGateway, code: code}
}

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

var (
	errEventFeedUnavailable = &codedError{
		msg:    "event feed unavailable",
		status: http.StatusServiceUnavailable,
		code:   "admin.event_feed_unavailable",
	}
	errStreamSubscriberLimit = &codedError{
		msg:    "too many admin streams open",
		status: http.StatusTooManyRequests,
		code:   "admin.stream_subscriber_limit",
	}
	errStreamingUnsupported = &codedError{
		msg:    "streaming unsupported",
		status: http.StatusInternalServerError,
		code:   "admin.streaming_unsupported",
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
)

// streamUnavailable codes a live-tail Subscribe failure that is not the
// subscriber ceiling: the stream exists but cannot be joined right now, so it
// answers with the retryable 503 every other unavailable admin path returns.
func streamUnavailable(stream string) *codedError {
	return &codedError{
		msg:    stream + " stream unavailable",
		status: http.StatusServiceUnavailable,
		code:   "admin.stream_unavailable",
	}
}

package domain

import "net/http"

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var (
	ErrPrincipalRequired = &CodedError{
		Msg:    "overseer principal required",
		Status: http.StatusForbidden,
		Code:   "observe.principal_required",
	}
	ErrDiscographyQualityTimeout = &CodedError{
		Msg:    "discography quality query timed out",
		Status: http.StatusGatewayTimeout,
		Code:   "observe.discography_quality_timeout",
	}
	ErrEventFeedUnavailable = &CodedError{
		Msg:    "event feed unavailable",
		Status: http.StatusServiceUnavailable,
		Code:   "observe.event_feed_unavailable",
	}
	ErrStreamSubscriberLimit = &CodedError{
		Msg:    "too many observe streams open",
		Status: http.StatusTooManyRequests,
		Code:   "observe.stream_subscriber_limit",
	}
	ErrStreamingUnsupported = &CodedError{
		Msg:    "streaming unsupported",
		Status: http.StatusInternalServerError,
		Code:   "observe.streaming_unsupported",
	}
)

func StreamUnavailable(stream string) *CodedError {
	return &CodedError{
		Msg:    stream + " stream unavailable",
		Status: http.StatusServiceUnavailable,
		Code:   "observe.stream_unavailable",
	}
}

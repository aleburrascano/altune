package handler

import (
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const (
	// statusClientClosedRequest is nginx's 499: the caller went away before the
	// answer, so the request is not a backend failure and must not be counted
	// as one.
	statusClientClosedRequest = 499

	// unclassifiedInspectorDetail is all an unrecognised inspector failure
	// tells the caller. The real cause is frequently a transport error carrying
	// a provider URL and its credentials, so it goes to the log only (#2006).
	unclassifiedInspectorDetail = "inspector request failed"
)

// codedError carries a stable, machine-checkable error code alongside its HTTP
// status and message. It implements httputil.StatusError, httputil.ErrorCoder
// and httputil.RetryAfterer so admin responses routed through
// httputil.HandleServiceError gain a `code` field, matching the metrics
// handler, and a Retry-After when the wait is known.
type codedError struct {
	msg        string
	status     int
	code       string
	retryAfter time.Duration
}

func (e *codedError) Error() string     { return e.msg }
func (e *codedError) HTTPStatus() int   { return e.status }
func (e *codedError) ErrorCode() string { return e.code }

// RetryAfter is the wait a refused caller should honour; zero (every error that
// is not a throttle) sets no header.
func (e *codedError) RetryAfter() time.Duration { return e.retryAfter }

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
	errReplaySlotsBusy = &codedError{
		msg:        "too many inspector replays running",
		status:     http.StatusTooManyRequests,
		code:       "admin.inspector_busy",
		retryAfter: busyRetryAfter,
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
	errQueryRequired = &codedError{
		msg:    "query is required",
		status: http.StatusBadRequest,
		code:   "admin.query_required",
	}
	errTooManyKinds = &codedError{
		msg:    "kinds has too many entries",
		status: http.StatusBadRequest,
		code:   "admin.invalid_request",
	}
	errInvalidJSON = &codedError{
		msg:    "request body is not valid json",
		status: http.StatusBadRequest,
		code:   "admin.invalid_json",
	}
	errBodyTooLarge = &codedError{
		msg:    "request body too large",
		status: http.StatusRequestEntityTooLarge,
		code:   "admin.body_too_large",
	}
	errInspectorTimeout = &codedError{
		msg:    "inspector request timed out",
		status: http.StatusGatewayTimeout,
		code:   "admin.timeout",
	}
	errClientClosedRequest = &codedError{
		msg:    "client closed request",
		status: statusClientClosedRequest,
		code:   "admin.client_closed_request",
	}
	errAllProvidersFailed = &codedError{
		msg:    "all discovery providers failed",
		status: http.StatusBadGateway,
		code:   "admin.all_providers_failed",
	}
	errRequestNotFound = &codedError{
		msg:    "request not found",
		status: http.StatusNotFound,
		code:   "admin.request_not_found",
	}
)

// replayThrottled codes an operator that has spent its inspector replay budget,
// carrying the wait until its next token so a client retries once rather than
// spinning against the limit.
func replayThrottled(wait time.Duration) *codedError {
	return &codedError{
		msg:        "too many inspector replays, try again later",
		status:     http.StatusTooManyRequests,
		code:       "admin.inspector_throttled",
		retryAfter: wait,
	}
}

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

// ErrInspectorInvalidInput marks a rerun/test-search/rerun-detail failure caused
// by the caller's input (unknown kinds, empty or oversized query). The inspector
// wiring wraps such errors with it so the handler answers 400, not 502.
var ErrInspectorInvalidInput = errors.New("invalid inspector request")

// ErrInspectorProvidersDown marks an inspector failure caused by every discovery
// provider failing, so an outage carries its own 502 code distinct from both a
// bad request and any other upstream failure.
var ErrInspectorProvidersDown = errors.New("all providers failed")

// inspectorError maps an inspector failure to its coded response. The caller's
// own abort outranks whatever the inspector reported: a provider error observed
// after the deadline hit describes the symptom, not the cause (#2006).
func inspectorError(ctx context.Context, failCode string, err error) *codedError {
	if errors.Is(err, ErrInspectorInvalidInput) {
		return invalidInspectorRequest(err)
	}
	if aborted := requestAbort(ctx, err); aborted != nil {
		return abortError(aborted)
	}
	if errors.Is(err, ErrInspectorProvidersDown) {
		return errAllProvidersFailed
	}
	return unclassifiedInspectorError(ctx, failCode, err)
}

// invalidInspectorRequest answers bad input with the validation wording, which
// describes the caller's own request (#1021), masked in case that request
// carried a credential.
func invalidInspectorRequest(err error) *codedError {
	return &codedError{
		msg:    redact.Secrets(err.Error()),
		status: http.StatusBadRequest,
		code:   "admin.invalid_request",
	}
}

// requestAbort names the caller-side abort behind an inspector failure, taken
// from the request context's own outcome or from a context error the inspector
// wrapped, and nil when the failure is not an abort.
func requestAbort(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		return nil
	}
}

// abortError separates a deadline, which the operator can retry against a
// longer budget, from the caller's own disconnect.
func abortError(aborted error) *codedError {
	if errors.Is(aborted, context.DeadlineExceeded) {
		return errInspectorTimeout
	}
	return errClientClosedRequest
}

// unclassifiedInspectorError keeps an unrecognised failure's real cause in the
// log and answers with the endpoint's stable code, so a wrapped provider URL or
// API key cannot reach the response body (#2006).
func unclassifiedInspectorError(ctx context.Context, failCode string, err error) *codedError {
	slog.ErrorContext(ctx, "admin.inspector_failed",
		slog.String("code", failCode),
		slog.String("error", redact.Secrets(err.Error())),
	)
	return &codedError{msg: unclassifiedInspectorDetail, status: http.StatusBadGateway, code: failCode}
}

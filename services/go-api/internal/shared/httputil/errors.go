package httputil

import (
	"altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	internalServerErrorDetail = "internal server error"
	problemContentType        = "application/problem+json"
	problemTypePrefix         = "urn:altune:problem:"
	statusClientClosedRequest = 499
)

type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

type ErrorResponse struct {
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`
}

type StatusError interface {
	error
	HTTPStatus() int
}

type ErrorCoder interface {
	ErrorCode() string
}

type clientDetailer interface {
	ClientDetail() string
}

type retryAfterer interface {
	RetryAfter() time.Duration
}

var kindStatus = map[domain.Kind]int{
	domain.KindInternal:        http.StatusInternalServerError,
	domain.KindInvalid:         http.StatusBadRequest,
	domain.KindUnauthenticated: http.StatusUnauthorized,
	domain.KindForbidden:       http.StatusForbidden,
	domain.KindNotFound:        http.StatusNotFound,
	domain.KindConflict:        http.StatusConflict,
	domain.KindTooLarge:        http.StatusRequestEntityTooLarge,
	domain.KindRateLimited:     http.StatusTooManyRequests,
	domain.KindUpstreamFailed:  http.StatusBadGateway,
	domain.KindUnavailable:     http.StatusServiceUnavailable,
	domain.KindTimeout:         http.StatusGatewayTimeout,
}

func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if clientLeft(r, err) {
		slog.InfoContext(r.Context(), "request.cancelled",
			logging.KeyMethod, r.Method, logging.KeyPath, r.URL.Path, logging.KeyError, err)
		w.WriteHeader(statusClientClosedRequest)
		return
	}
	p := problemFor(r, err)
	logFailure(r, p, err)
	setRetryAfter(w.Header(), err)
	writeProblem(w, p)
}

func clientLeft(r *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled)
}

func ranOutOfTime(r *http.Request, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && errors.Is(r.Context().Err(), context.DeadlineExceeded)
}

func problemFor(r *http.Request, err error) Problem {
	var coded *domain.CodedError
	if errors.As(err, &coded) {
		return newProblem(r, coded.Code(), coded.Error())
	}
	var se StatusError
	if errors.As(err, &se) {
		return legacyProblem(r, err, se)
	}
	if ranOutOfTime(r, err) {
		return newProblem(r, domain.CodeRequestTimeout, "")
	}
	return newProblem(r, domain.CodeInternal, internalServerErrorDetail)
}

func newProblem(r *http.Request, code domain.Code, detail string) Problem {
	if detail == "" {
		detail = code.Title()
	}
	return Problem{
		Type:     problemTypePrefix + code.String(),
		Title:    code.Title(),
		Status:   statusOf(code.Kind()),
		Detail:   detail,
		Instance: logging.CorrelationIDFromContext(r.Context()),
		Code:     code.String(),
	}
}

func statusOf(kind domain.Kind) int {
	if status, ok := kindStatus[kind]; ok {
		return status
	}
	return http.StatusInternalServerError
}

func legacyProblem(r *http.Request, err error, se StatusError) Problem {
	status := se.HTTPStatus()
	code := legacyCode(err, status)
	return Problem{
		Type:     problemTypePrefix + code,
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   legacyDetail(err, se),
		Instance: logging.CorrelationIDFromContext(r.Context()),
		Code:     code,
	}
}

func legacyDetail(err error, se StatusError) string {
	var detailer clientDetailer
	if errors.As(err, &detailer) {
		return detailer.ClientDetail()
	}
	return se.Error()
}

func legacyCode(err error, status int) string {
	var coder ErrorCoder
	if errors.As(err, &coder) {
		if code := coder.ErrorCode(); code != "" {
			return code
		}
	}
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	default:
		return "error"
	}
}

func logFailure(r *http.Request, p Problem, err error) {
	level, message := slog.LevelDebug, "request.rejected"
	switch {
	case p.Code == domain.CodeRequestTimeout.String():
		level, message = slog.LevelWarn, "request.timed_out"
	case p.Status >= http.StatusInternalServerError:
		level, message = slog.LevelError, "request.failed"
	}
	slog.Log(r.Context(), level, message,
		logging.KeyMethod, r.Method,
		logging.KeyPath, r.URL.Path,
		logging.KeyStatus, p.Status,
		logging.KeyCode, p.Code,
		logging.KeyError, err)
}

func writeProblem(w http.ResponseWriter, p Problem) {
	body, _ := json.Marshal(p)
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}

func setRetryAfter(h http.Header, err error) {
	var retryable retryAfterer
	if !errors.As(err, &retryable) || h.Get("Retry-After") != "" {
		return
	}
	if wait := retryable.RetryAfter(); wait > 0 {
		h.Set("Retry-After", RetryAfterSeconds(wait))
	}
}

func RetryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("encode %T response: %v", v, err))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeDetail(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Detail: message})
}

func NotFound(w http.ResponseWriter, message string) {
	if message == "" {
		message = "not found"
	}
	writeDetail(w, http.StatusNotFound, message)
}

func BadRequest(w http.ResponseWriter, message string) {
	writeDetail(w, http.StatusBadRequest, message)
}

func InternalError(w http.ResponseWriter, msgs ...string) {
	msg := internalServerErrorDetail
	if len(msgs) > 0 && msgs[0] != "" {
		msg = msgs[0]
	}
	writeDetail(w, http.StatusInternalServerError, msg)
}

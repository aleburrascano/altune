package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

const internalServerErrorDetail = "internal server error"

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

type ClientDetailer interface {
	ClientDetail() string
}

// RetryAfterer is implemented by an error whose caller may retry once a known
// wait has passed; the wait reaches the client as a Retry-After header.
type RetryAfterer interface {
	RetryAfter() time.Duration
}

func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var se StatusError
	if errors.As(err, &se) {
		writeStatusError(w, r, err, se)
		return
	}
	slog.ErrorContext(r.Context(), "service.unhandled_error",
		"method", r.Method, "path", r.URL.Path, "error", err)
	WriteJSON(w, http.StatusInternalServerError, ErrorResponse{
		Detail: internalServerErrorDetail,
		Code:   "internal",
	})
}

func writeStatusError(w http.ResponseWriter, r *http.Request, err error, se StatusError) {
	status := se.HTTPStatus()
	code := resolveErrorCode(err, status)
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "service.upstream_error",
			"method", r.Method, "path", r.URL.Path, "status", status, "code", code, "error", err)
	}
	setRetryAfter(w.Header(), err)
	WriteJSON(w, status, ErrorResponse{
		Detail: resolveDetail(err, se),
		Code:   code,
	})
}

// setRetryAfter rounds the wait up to whole seconds, so a client that honors
// the header retries after the wait has passed rather than just before. A
// header a caller already set wins: it knows the more precise wait.
func setRetryAfter(h http.Header, err error) {
	var retryable RetryAfterer
	if !errors.As(err, &retryable) || h.Get("Retry-After") != "" {
		return
	}
	if wait := retryable.RetryAfter(); wait > 0 {
		h.Set("Retry-After", strconv.FormatInt(int64(math.Ceil(wait.Seconds())), 10))
	}
}

func resolveDetail(err error, se StatusError) string {
	var detailer ClientDetailer
	if errors.As(err, &detailer) {
		return detailer.ClientDetail()
	}
	return se.Error()
}

func resolveErrorCode(err error, status int) string {
	var coder ErrorCoder
	if errors.As(err, &coder) {
		if code := coder.ErrorCode(); code != "" {
			return code
		}
	}
	return statusFallbackCode(status)
}

func statusFallbackCode(status int) string {
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

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("response.write_failed", "status", status, "error", err)
	}
}

func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Detail: message})
}

func NotFound(w http.ResponseWriter, message string) {
	if message == "" {
		message = "not found"
	}
	WriteError(w, http.StatusNotFound, message)
}

func BadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, message)
}

// BadRequestCode rejects a request with a code the caller can branch on, for a
// validation failure caught at the boundary, where no error value exists to
// carry the code through HandleServiceError.
func BadRequestCode(w http.ResponseWriter, code, message string) {
	WriteJSON(w, http.StatusBadRequest, ErrorResponse{Detail: message, Code: code})
}

func InternalError(w http.ResponseWriter, msgs ...string) {
	msg := internalServerErrorDetail
	if len(msgs) > 0 && msgs[0] != "" {
		msg = msgs[0]
	}
	WriteError(w, http.StatusInternalServerError, msg)
}

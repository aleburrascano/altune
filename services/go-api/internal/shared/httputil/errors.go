package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var se StatusError
	if errors.As(err, &se) {
		WriteJSON(w, se.HTTPStatus(), ErrorResponse{
			Detail: se.Error(),
			Code:   resolveErrorCode(err, se.HTTPStatus()),
		})
		return
	}
	slog.ErrorContext(r.Context(), "service.unhandled_error",
		"method", r.Method, "path", r.URL.Path, "error", err)
	WriteJSON(w, http.StatusInternalServerError, ErrorResponse{
		Detail: internalServerErrorDetail,
		Code:   "internal",
	})
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

func Forbidden(w http.ResponseWriter, message string) {
	if message == "" {
		message = "forbidden"
	}
	WriteError(w, http.StatusForbidden, message)
}

func InternalError(w http.ResponseWriter, msgs ...string) {
	msg := internalServerErrorDetail
	if len(msgs) > 0 && msgs[0] != "" {
		msg = msgs[0]
	}
	WriteError(w, http.StatusInternalServerError, msg)
}

func Conflict(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusConflict, message)
}

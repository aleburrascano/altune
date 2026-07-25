package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type ErrorResponse struct {
	Detail string `json:"detail"`
}

type StatusError interface {
	error
	HTTPStatus() int
}

func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var se StatusError
	if errors.As(err, &se) {
		WriteError(w, se.HTTPStatus(), se.Error())
		return
	}
	slog.ErrorContext(r.Context(), "service.unhandled_error",
		"method", r.Method, "path", r.URL.Path, "error", err)
	InternalError(w)
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

func Unauthorized(w http.ResponseWriter, message string) {
	if message == "" {
		message = "unauthorized"
	}
	WriteError(w, http.StatusUnauthorized, message)
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
	msg := "internal server error"
	if len(msgs) > 0 && msgs[0] != "" {
		msg = msgs[0]
	}
	WriteError(w, http.StatusInternalServerError, msg)
}

func Conflict(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusConflict, message)
}

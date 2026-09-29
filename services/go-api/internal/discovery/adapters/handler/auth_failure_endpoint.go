package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	requestCodeAuthFailureInvalidBody = "auth_failure.invalid_body"
	maxAuthFailureBodyBytes           = 512
)

var DefaultAuthFailureLimit = RequestLimit{Max: 10, Window: time.Minute}

var authFailureReasons = map[string]struct{}{
	"invalid_credentials": {},
	"email_not_confirmed": {},
	"network":             {},
	"too_many_attempts":   {},
	"unknown":             {},
}

var authFailureAppVersion = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,32}$`)

type authFailureRequest struct {
	Reason     *string `json:"reason"`
	AppVersion *string `json:"app_version"`
}

func (l *userRateLimiter) clientMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wait, admitted := l.admit(auth.ClientKey(r)); !admitted {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
			httputil.HandleServiceError(w, r, rateLimitedError{})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *DiscoveryHandler) PublicRoutes(r chi.Router, limit RequestLimit) {
	limiter := newUserRateLimiter(limit, time.Now)
	r.With(limiter.clientMiddleware, httputil.MaxBodySize(maxAuthFailureBodyBytes)).
		Post("/public/auth-failures", h.handleAuthFailure)
}

func decodeAuthFailure(r *http.Request) (reason, appVersion string, ok bool) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req authFailureRequest
	if err := dec.Decode(&req); err != nil {
		return "", "", false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", "", false
	}
	if req.Reason == nil || req.AppVersion == nil {
		return "", "", false
	}
	if _, known := authFailureReasons[*req.Reason]; !known {
		return "", "", false
	}
	if !authFailureAppVersion.MatchString(*req.AppVersion) {
		return "", "", false
	}
	return *req.Reason, *req.AppVersion, true
}

func (h *DiscoveryHandler) handleAuthFailure(w http.ResponseWriter, r *http.Request) {
	reason, appVersion, ok := decodeAuthFailure(r)
	if !ok {
		httputil.BadRequestCode(w, requestCodeAuthFailureInvalidBody, "invalid request body")
		return
	}

	if h.eventSvc == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.eventSvc.RecordAnonymousAuthFailure(r.Context(), reason, appVersion); err != nil {
		slog.ErrorContext(r.Context(), "record anonymous auth failure failed", "reason", reason, "error", err)
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

package handler

import (
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	maxAuthFailureBodyBytes = 512
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

func (h *DiscoveryHandler) PublicRoutes(r chi.Router, limit RequestLimit) {
	r.With(newClientRateLimiter(limit, h.now), httpserver.MaxBodySize(maxAuthFailureBodyBytes)).
		Post("/public/auth-failures", h.handleAuthFailure)
}

func validAuthFailure(req authFailureRequest) bool {
	if req.Reason == nil || req.AppVersion == nil {
		return false
	}
	if _, known := authFailureReasons[*req.Reason]; !known {
		return false
	}
	return authFailureAppVersion.MatchString(*req.AppVersion)
}

func (h *DiscoveryHandler) handleAuthFailure(w http.ResponseWriter, r *http.Request) {
	var req authFailureRequest
	if !httpserver.DecodeJSONWithCode(w, r, &req, apierror.CodeAuthFailureInvalidBody) {
		return
	}
	if !validAuthFailure(req) {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeAuthFailureInvalidBody, "invalid request body"))
		return
	}
	reason, appVersion := *req.Reason, *req.AppVersion

	if err := h.eventSvc.RecordAnonymousAuthFailure(r.Context(), reason, appVersion); err != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("record anonymous auth failure %q: %w", reason, err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

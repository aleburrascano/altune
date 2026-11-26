package handler

import (
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type authFailureRequest struct {
	Reason     *string `json:"reason"`
	AppVersion *string `json:"app_version"`
}

func (h *DiscoveryHandler) PublicRoutes(r chi.Router, limit RequestLimit) {
	r.With(newClientRateLimiter(limit, h.now), httpserver.MaxBodySize(maxAuthFailureBodyBytes)).
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
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeAuthFailureInvalidBody, "invalid request body"))
		return
	}

	if err := h.eventSvc.RecordAnonymousAuthFailure(r.Context(), reason, appVersion); err != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("record anonymous auth failure %q: %w", reason, err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

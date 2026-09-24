package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxReportBodyBytes = 32 << 10

type FeedbackHandler struct {
	submit *service.SubmitReportService
}

func NewFeedbackHandler(submit *service.SubmitReportService) *FeedbackHandler {
	return &FeedbackHandler{submit: submit}
}

func (h *FeedbackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(httputil.MaxBodySize(maxReportBodyBytes))
	r.Post("/reports", h.handleSubmitReport)
	return r
}

type SubmitReportRequest struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
	OSVersion  string `json:"os_version"`
	Screen     string `json:"screen"`
}

type SubmitReportResponse struct {
	IssueNumber int    `json:"issue_number"`
	IssueURL    string `json:"issue_url"`
}

func (h *FeedbackHandler) handleSubmitReport(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	var req SubmitReportRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	ref, err := h.submit.Execute(r.Context(), userId, service.SubmitReportInput{
		Kind:           req.Kind,
		Message:        req.Message,
		Diagnostics:    domain.NewDiagnostics(req.AppVersion, req.Platform, req.OSVersion, req.Screen),
		IdempotencyKey: idempotencyKey(r),
	})
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, SubmitReportResponse{
		IssueNumber: ref.Number,
		IssueURL:    ref.URL,
	})
}

// idempotencyKey reads the optional client-supplied Idempotency-Key header. A
// blank or absent header yields nil, meaning every submit creates a fresh
// issue; a present key collapses a retry or double-tapped Submit onto the first
// issue. Mirrors the catalog track-create handler's header.
func idempotencyKey(r *http.Request) *string {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return nil
	}
	return &key
}

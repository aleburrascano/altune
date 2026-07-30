package handler

import (
	"encoding/json"
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type FeedbackHandler struct {
	submit *service.SubmitReportService
}

func NewFeedbackHandler(submit *service.SubmitReportService) *FeedbackHandler {
	return &FeedbackHandler{submit: submit}
}

func (h *FeedbackHandler) Routes() chi.Router {
	r := chi.NewRouter()
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	ref, err := h.submit.Execute(r.Context(), userId, service.SubmitReportInput{
		Kind:    req.Kind,
		Message: req.Message,
		Diagnostics: domain.Diagnostics{
			AppVersion: req.AppVersion,
			Platform:   req.Platform,
			OSVersion:  req.OSVersion,
			Screen:     req.Screen,
		},
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

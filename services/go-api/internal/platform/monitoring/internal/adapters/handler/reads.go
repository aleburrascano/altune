package handler

import (
	"altune/go-api/internal/platform/monitoring/service/searchqualitycheck"
	"altune/go-api/internal/shared/httpserver"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) registerReads(r chi.Router) {
	r.Get("/metrics/live", h.serveMetricsLive)
	r.Get("/eval", h.serveEval)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/quality/discography", h.serveDiscographyQuality)
}

func (h *Handler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, readAcquisitionStatus(h.deps.Acquisition))
}

func (h *Handler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Eval == nil {
		httpserver.WriteJSON(w, http.StatusOK, searchqualitycheck.Status{Enabled: false, State: searchqualitycheck.StateDisabled})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, h.deps.Eval.Status())
}

func (h *Handler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	if h.deps.LiveMetrics == nil {
		httpserver.WriteJSON(w, http.StatusOK, struct{}{})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, h.deps.LiveMetrics())
}

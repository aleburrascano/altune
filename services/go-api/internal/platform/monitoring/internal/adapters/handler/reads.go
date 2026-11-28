package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/httpserver"
	"net/http"
)

func (h *Handler) serveAcquisition(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, readAcquisitionStatus(r.Context(), h.deps.Acquisition))
}

func (h *Handler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Eval == nil {
		httpserver.WriteJSON(w, http.StatusOK, service.MeterStatus{Enabled: false, State: service.StateDisabled})
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

package handler

import (
	"altune/go-api/internal/observe/adapters/acquisitionbridge"
	"altune/go-api/internal/observe/service/evalmeter"
	"altune/go-api/internal/shared/httputil"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) registerReads(r chi.Router) {
	r.Get("/metrics/live", h.serveMetricsLive)
	r.Get("/eval", h.serveEval)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/quality/discography", h.serveDiscographyQuality)
}

type acquisitionStatusDTO = acquisitionbridge.AcquisitionStatusDTO

func (h *Handler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, acquisitionbridge.ReadAcquisitionStatus(h.deps.Acquisition))
}

func (h *Handler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Eval == nil {
		httputil.WriteJSON(w, http.StatusOK, evalmeter.Status{Enabled: false, State: evalmeter.StateDisabled})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.deps.Eval.Status())
}

func (h *Handler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	if h.deps.LiveMetrics == nil {
		httputil.WriteJSON(w, http.StatusOK, struct{}{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.deps.LiveMetrics())
}

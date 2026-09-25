package handler

import "github.com/go-chi/chi/v5"

func (h *Handler) registerReads(r chi.Router) {
	r.Get("/metrics/live", h.serveMetricsLive)
	r.Get("/eval", h.serveEval)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/quality/discography", h.serveDiscographyQuality)
}

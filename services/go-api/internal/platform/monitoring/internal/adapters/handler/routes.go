package handler

import (
	"altune/go-api/internal/shared/httpserver"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Register(r chi.Router) {
	r.Get("/health", h.serveHealth)
	h.registerReads(r)
	h.registerStreams(r)
}

func (h *Handler) registerReads(r chi.Router) {
	r.Get("/metrics/live", h.serveMetricsLive)
	r.Get("/eval", h.serveEval)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/quality/discography", h.serveDiscographyQuality)
}

func (h *Handler) registerStreams(r chi.Router) {
	r.With(auditStreamOpen).Get("/events/stream", h.streamEvents)
	r.With(auditStreamOpen).Get("/logs/stream", h.streamLogs)
}

func (h *ClientEventHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.limiter, httpserver.MaxBodySize(maxClientEventBodyBytes)).Post("/events", h.handleRecord)
	return r
}

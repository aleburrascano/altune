package handler

import (
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared/logging"
	"net/http"
	"time"

	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/discovery/ports"

	"github.com/go-chi/chi/v5"
)

type LiveMetrics map[string]any

type LiveMetricsSource func() LiveMetrics

type AcquisitionReader interface {
	Status() acqPorts.AcquisitionStatus
}

type Deps struct {
	Health      HealthProbe
	Logs        *logging.RingBuffer
	Events      *eventtap.Feed
	Eval        *evalmeter.Meter
	Acquisition AcquisitionReader
	LiveMetrics LiveMetricsSource
	Discography ports.DiscographyQualityReader
	Shutdown    <-chan struct{}
}

type Handler struct {
	deps         Deps
	probeTimeout time.Duration
}

func New(d Deps) *Handler {
	return &Handler{deps: d, probeTimeout: defaultProbeTimeout}
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/health", h.serveHealth)
	h.registerReads(r)
	h.registerStreams(r)
}

func NoStoreAndNosniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

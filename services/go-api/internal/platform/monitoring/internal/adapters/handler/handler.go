package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/platform/monitoring/internal/shell/eventwatch"
	"altune/go-api/internal/platform/monitoring/service/searchqualitycheck"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type LiveMetrics map[string]any

type LiveMetricsSource func() LiveMetrics

type Deps struct {
	Health      HealthProbe
	Logs        *logging.RingBuffer
	Events      *eventwatch.WatchedEvents
	Eval        *searchqualitycheck.Meter
	Acquisition ports.AcquisitionReader
	LiveMetrics LiveMetricsSource
	Discography ports.DiscographyQualityReader
	Shutdown    <-chan struct{}
}

type Handler struct {
	now          clock.Func
	deps         Deps
	probeTimeout time.Duration
	streams      StreamBounds
}

func NewObserveHandler(now clock.Func, d Deps) *Handler {
	if now == nil {
		panic("handler.NewObserveHandler: a nil clock")
	}
	return &Handler{now: now, deps: d, probeTimeout: defaultProbeTimeout, streams: DefaultStreamBounds()}
}

func (h *Handler) WithStreamBounds(bounds StreamBounds) *Handler {
	h.streams = StreamBounds{
		WriteIdle:   positiveOr(bounds.WriteIdle, h.streams.WriteIdle),
		Heartbeat:   positiveOr(bounds.Heartbeat, h.streams.Heartbeat),
		MaxLifetime: positiveOr(bounds.MaxLifetime, h.streams.MaxLifetime),
	}
	return h
}

func positiveOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
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

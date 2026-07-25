package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/admin/ui"
	"altune/go-api/internal/shared/logging"
)

type AdminHandler struct {
	probe   HealthProbe
	logRing *logging.RingBuffer

	eventFeed       *eventtap.Feed
	providerHealth  *providerhealth.Store
	acquisition     AcquisitionStatusReader
	evalMeter       *evalmeter.Meter
	requests        *requeststore.Store
	reRunner        ReRunner
	searchInspector SearchInspector
	detailReRunner  DetailReRunner
	metricsHistory  MetricsHistoryReader

	supabaseURL     string
	supabaseAnonKey string
}

func New(probe HealthProbe, logRing *logging.RingBuffer) *AdminHandler {
	return &AdminHandler{probe: probe, logRing: logRing}
}

func (h *AdminHandler) WithEventFeed(f *eventtap.Feed) *AdminHandler {
	h.eventFeed = f
	return h
}

func (h *AdminHandler) WithProviderHealth(s *providerhealth.Store) *AdminHandler {
	h.providerHealth = s
	return h
}

func (h *AdminHandler) WithAcquisition(r AcquisitionStatusReader) *AdminHandler {
	h.acquisition = r
	return h
}

func (h *AdminHandler) WithEvalMeter(m *evalmeter.Meter) *AdminHandler {
	h.evalMeter = m
	return h
}

func (h *AdminHandler) WithRequestStore(r *requeststore.Store) *AdminHandler {
	h.requests = r
	return h
}

func (h *AdminHandler) WithSupabaseLogin(url, anonKey string) *AdminHandler {
	h.supabaseURL = url
	h.supabaseAnonKey = anonKey
	return h
}

func (h *AdminHandler) ServeIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.IndexHTML))
}

func (h *AdminHandler) RegisterData(r chi.Router) {
	r.Get("/health", h.serveHealth)
	r.Get("/logs", h.serveLogs)
	r.Get("/logs/stream", h.streamLogs)
	r.Get("/events/rates", h.serveEventRates)
	r.Get("/events/stream", h.streamEvents)
	r.Get("/providers", h.serveProviders)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/eval", h.serveEval)
	r.Get("/metrics", h.serveMetricsHistory)
	r.Get("/requests", h.serveRequests)
	r.Get("/requests/{corrID}", h.serveRequestDetail)
	r.Post("/rerun", h.serveReRun)
	r.Post("/rerun-detail", h.serveReRunDetail)
	r.Post("/search", h.serveTestSearch)
}

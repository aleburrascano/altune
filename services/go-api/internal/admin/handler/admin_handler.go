package handler

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type AdminHandler struct {
	probe        HealthProbe
	probeTimeout time.Duration
	logRing      *logging.RingBuffer

	eventFeed      *eventtap.Feed
	acquisition    AcquisitionController
	evalMeter      *evalmeter.Meter
	alertMonitor   *alert.Monitor
	jobs           JobSwitchboard
	metricsHistory ports.MetricsRollupStore
	liveMetrics    LiveMetricsSource
	// metricsHistoryTimeout bounds the metrics-history store call.
	metricsHistoryTimeout time.Duration

	discographyQuality ports.DiscographyQualityReader

	shutdown <-chan struct{}
}

// New requires a non-nil probe and logRing: /health invokes the probe and the
// /logs routes dereference the ring on every request, neither behind a nil
// guard. Every other dependency arrives through a With* method and has the
// degraded answer AdminHandler documents.
func New(probe HealthProbe, logRing *logging.RingBuffer) *AdminHandler {
	return &AdminHandler{probe: probe, probeTimeout: defaultProbeTimeout, metricsHistoryTimeout: defaultMetricsHistoryTimeout, logRing: logRing}
}

func (h *AdminHandler) WithEventFeed(f *eventtap.Feed) *AdminHandler {
	h.eventFeed = f
	return h
}

func (h *AdminHandler) WithAcquisition(r AcquisitionController) *AdminHandler {
	h.acquisition = r
	return h
}

func (h *AdminHandler) WithEvalMeter(m *evalmeter.Meter) *AdminHandler {
	h.evalMeter = m
	return h
}

// WithAlertMonitor exposes the alert monitor's runtime kill switch on the
// operator-only /alerts routes.
func (h *AdminHandler) WithAlertMonitor(m *alert.Monitor) *AdminHandler {
	h.alertMonitor = m
	return h
}

func (h *AdminHandler) WithShutdown(done <-chan struct{}) *AdminHandler {
	h.shutdown = done
	return h
}

// NoStoreAndNosniff guards the whole /admin tree: its responses are answers to
// one authenticated operator, so a shared cache holding one, or a browser
// sniffing a JSON body into a document, hands that operator's view to whoever
// comes next. A stream handler overwrites Cache-Control with its own no-cache.
func NoStoreAndNosniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (h *AdminHandler) RegisterData(r chi.Router) {
	r.Get("/health", h.serveHealth)
	r.With(auditDataRead).Get("/logs/stream", h.streamLogs)
	r.With(auditDataRead).Get("/events/stream", h.streamEvents)
	r.Get("/acquisition", h.serveAcquisition)
	r.Post("/acquisition/pause", h.pauseAcquisition)
	r.Post("/acquisition/resume", h.resumeAcquisition)
	r.Get("/eval", h.serveEval)
	r.Post("/eval/pause", h.pauseEval)
	r.Post("/eval/resume", h.resumeEval)
	r.Get("/alerts", h.serveAlerts)
	r.Post("/alerts/pause", h.pauseAlerts)
	r.Post("/alerts/resume", h.resumeAlerts)
	r.Get("/jobs", h.serveJobs)
	r.Post("/jobs/{name}/enable", h.enableJob)
	r.Post("/jobs/{name}/disable", h.disableJob)
	r.Get("/metrics", h.serveMetricsHistory)
	r.Get("/metrics/live", h.serveMetricsLive)
	r.Get("/quality/discography", h.serveDiscographyQuality)
}

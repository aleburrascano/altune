package handler

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/admin/ui"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// AdminHandler serves the whole admin surface over dependencies that are all
// optional bar the two New takes. What a route answers when its dependency was
// never wired is part of the operator contract, because the console reads that
// answer rather than a wiring manifest:
//
//   - 503 with a code: the kill-switch POSTs (acquisition, eval and alerts
//     pause/resume), every /jobs route, /rerun, /rerun-detail, /search, and both
//     /events routes. The /events pair answers 503 for an unsubscribed feed too,
//     not only an absent one, so a tap that failed to subscribe cannot read as a
//     system with nothing to report.
//   - 404: GET /requests/{corrID}, which cannot distinguish an unwired store
//     from a trace already evicted.
//
// The 500 on /events/stream is not a nil dependency: it is the
// streaming-unsupported case, a ResponseWriter that cannot flush.
type AdminHandler struct {
	probe        HealthProbe
	probeTimeout time.Duration
	logRing      *logging.RingBuffer

	eventFeed       *eventtap.Feed
	providerHealth  *providerhealth.Store
	acquisition     AcquisitionController
	evalMeter       *evalmeter.Meter
	alertMonitor    *alert.Monitor
	jobs            JobSwitchboard
	requests        *requeststore.Store
	reRunner        ReRunner
	searchInspector SearchInspector
	detailReRunner  DetailReRunner
	metricsHistory  ports.MetricsRollupStore
	liveMetrics     LiveMetricsSource
	// metricsHistoryTimeout bounds the metrics-history store call.
	metricsHistoryTimeout time.Duration

	discographyQuality ports.DiscographyQualityReader

	supabaseURL     string
	supabaseAnonKey string
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

func (h *AdminHandler) WithProviderHealth(s *providerhealth.Store) *AdminHandler {
	h.providerHealth = s
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

func (h *AdminHandler) WithRequestStore(r *requeststore.Store) *AdminHandler {
	h.requests = r
	return h
}

func (h *AdminHandler) WithSupabaseLogin(projectURL, anonKey string) *AdminHandler {
	h.supabaseURL = projectURL
	h.supabaseAnonKey = anonKey
	return h
}

func (h *AdminHandler) ServeIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy(cspSourceOrigin(h.supabaseURL)))
	_, _ = w.Write([]byte(ui.IndexHTML))
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

// contentSecurityPolicy is the console's policy. The page keeps the operator's
// Supabase tokens in sessionStorage and renders provider-controlled strings
// through innerHTML, so frame-ancestors and a connect-src naming only this
// origin and the login host are what stop a framing page or a missed esc() from
// reaching those tokens. 'unsafe-inline' stays while the script lives in
// index.html; a nonce is #1995's named non-goal. An empty supabaseOrigin leaves
// connect-src at 'self' rather than widening it.
func contentSecurityPolicy(supabaseOrigin string) string {
	connect := "connect-src 'self'"
	if supabaseOrigin != "" {
		connect += " " + supabaseOrigin
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' https:",
		connect,
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"object-src 'none'",
	}, "; ")
}

// cspPolicyBreakers end a CSP source and begin the next one, so a configured
// URL containing any of them could append a directive of its author's choosing.
const cspPolicyBreakers = " \t\r\n;,'\"`"

// cspSourceOrigin reduces a configured URL to the scheme://host a CSP source may
// name, and returns "" for anything that is not a plain http(s) origin.
func cspSourceOrigin(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	origin := u.Scheme + "://" + u.Host
	if strings.ContainsAny(origin, cspPolicyBreakers) {
		return ""
	}
	return origin
}

func (h *AdminHandler) RegisterData(r chi.Router) {
	r.Get("/health", h.serveHealth)
	r.Get("/logs", h.serveLogs)
	r.Get("/logs/stream", h.streamLogs)
	r.Get("/events/rates", h.serveEventRates)
	r.Get("/events/stream", h.streamEvents)
	r.Get("/providers", h.serveProviders)
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
	r.Get("/requests", h.serveRequests)
	r.Get("/requests/{corrID}", h.serveRequestDetail)
	r.Post("/rerun", h.serveReRun)
	r.Post("/rerun-detail", h.serveReRunDetail)
	r.Post("/search", h.serveTestSearch)
}

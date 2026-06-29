// Package handler serves the Mission Control operator console under /admin.
//
// The console shell (ServeIndex) is unauthenticated — a browser cannot send a
// bearer token on navigation, and the shell holds no data: every panel fetches
// from the operator-gated data endpoints (RegisterData), which the composition
// root wraps with auth.Middleware + OperatorOnly. The token is supplied by the
// page and sent as an Authorization header on those data calls.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/admin/ui"
	"altune/go-api/internal/shared/logging"
)

// providerHealthReader is the read side of the provider status board, satisfied
// by *providerhealth.Store.
type providerHealthReader interface {
	Snapshot() []providerhealth.ProviderSnapshot
}

// requestStoreReader is the read side of the discovery request drill-down,
// satisfied by *requeststore.Store.
type requestStoreReader interface {
	Snapshot() []requeststore.Record
	Get(corrID string) (requeststore.Record, bool)
}

type AdminHandler struct {
	operatorUserID string
	probe          HealthProbe
	logRing        *logging.RingBuffer
	eventFeed      *EventFeed
	providerHealth providerHealthReader
	acquisition    AcquisitionStatusReader
	evalMeter      *EvalMeter
	requests        requestStoreReader
	reRunner        ReRunner
	searchInspector SearchInspector

	supabaseURL     string
	supabaseAnonKey string
}

// WithRequestStore attaches the discovery request-drill-down store. A nil store
// leaves the /requests endpoints answering empty.
func (h *AdminHandler) WithRequestStore(r requestStoreReader) *AdminHandler {
	h.requests = r
	return h
}

// WithSupabaseLogin attaches the public Supabase client config so the console
// page can sign the operator in with email + password directly. Both values are
// public client config, not secrets.
func (h *AdminHandler) WithSupabaseLogin(url, anonKey string) *AdminHandler {
	h.supabaseURL = url
	h.supabaseAnonKey = anonKey
	return h
}

func New(
	operatorUserID string,
	probe HealthProbe,
	logRing *logging.RingBuffer,
	eventFeed *EventFeed,
	providerHealth providerHealthReader,
	acquisition AcquisitionStatusReader,
	evalMeter *EvalMeter,
) *AdminHandler {
	return &AdminHandler{
		operatorUserID: operatorUserID,
		probe:          probe,
		logRing:        logRing,
		eventFeed:      eventFeed,
		providerHealth: providerHealth,
		acquisition:    acquisition,
		evalMeter:      evalMeter,
	}
}

// ServeIndex serves the unauthenticated console shell.
func (h *AdminHandler) ServeIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.IndexHTML))
}

// RegisterData registers the operator-gated data + stream endpoints onto r. The
// caller applies auth.Middleware + OperatorOnly ahead of r (the two-layer gate).
func (h *AdminHandler) RegisterData(r chi.Router) {
	r.Get("/health", h.serveHealth)
	r.Get("/logs", h.serveLogs)
	r.Get("/logs/stream", h.streamLogs)
	r.Get("/events/rates", h.serveEventRates)
	r.Get("/events/stream", h.streamEvents)
	r.Get("/providers", h.serveProviders)
	r.Get("/acquisition", h.serveAcquisition)
	r.Get("/eval", h.serveEval)
	r.Get("/requests", h.serveRequests)
	r.Get("/requests/{corrID}", h.serveRequestDetail)
	r.Post("/rerun", h.serveReRun)
	r.Post("/search", h.serveTestSearch)
}

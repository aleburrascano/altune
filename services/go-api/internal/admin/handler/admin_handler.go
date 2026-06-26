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
	"altune/go-api/internal/admin/ui"
	"altune/go-api/internal/shared/logging"
)

// providerHealthReader is the read side of the provider status board, satisfied
// by *providerhealth.Store.
type providerHealthReader interface {
	Snapshot() []providerhealth.ProviderSnapshot
}

type AdminHandler struct {
	operatorUserID string
	probe          HealthProbe
	logRing        *logging.RingBuffer
	eventFeed      *EventFeed
	providerHealth providerHealthReader
	acquisition    AcquisitionStatusReader
	evalMeter      *EvalMeter
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
}

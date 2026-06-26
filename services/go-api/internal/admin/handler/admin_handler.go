// Package handler serves the Mission Control operator console under /admin.
//
// The console is operator-only: callers must be authenticated (auth.Middleware,
// applied by the composition root) and match the configured operator account
// (OperatorOnly, applied here). Panel endpoints mount onto Routes() as later
// units land; today it serves the console page.
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
}

func New(
	operatorUserID string,
	probe HealthProbe,
	logRing *logging.RingBuffer,
	eventFeed *EventFeed,
	providerHealth providerHealthReader,
	acquisition AcquisitionStatusReader,
) *AdminHandler {
	return &AdminHandler{
		operatorUserID: operatorUserID,
		probe:          probe,
		logRing:        logRing,
		eventFeed:      eventFeed,
		providerHealth: providerHealth,
		acquisition:    acquisition,
	}
}

// Routes returns the operator-gated console router. The caller is responsible
// for applying auth.Middleware ahead of this router (the two-layer stack); this
// router applies the operator gate.
func (h *AdminHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(OperatorOnly(h.operatorUserID))
	r.Get("/", h.serveIndex)
	r.Get("/health", h.serveHealth)
	r.Get("/logs", h.serveLogs)
	r.Get("/logs/stream", h.streamLogs)
	r.Get("/events/rates", h.serveEventRates)
	r.Get("/events/stream", h.streamEvents)
	r.Get("/providers", h.serveProviders)
	r.Get("/acquisition", h.serveAcquisition)
	return r
}

func (h *AdminHandler) serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.IndexHTML))
}

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

	"altune/go-api/internal/admin/ui"
)

type AdminHandler struct {
	operatorUserID string
	probe          HealthProbe
}

func New(operatorUserID string, probe HealthProbe) *AdminHandler {
	return &AdminHandler{operatorUserID: operatorUserID, probe: probe}
}

// Routes returns the operator-gated console router. The caller is responsible
// for applying auth.Middleware ahead of this router (the two-layer stack); this
// router applies the operator gate.
func (h *AdminHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(OperatorOnly(h.operatorUserID))
	r.Get("/", h.serveIndex)
	r.Get("/health", h.serveHealth)
	return r
}

func (h *AdminHandler) serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(ui.IndexHTML))
}

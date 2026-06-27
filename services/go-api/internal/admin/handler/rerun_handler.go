package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
)

// ReRunResult is the full waterfall of an on-demand re-run, mirroring the passive
// drill-down's projection so the console renders both the same way.
type ReRunResult struct {
	Query     string                       `json:"query"`
	Kinds     []string                     `json:"kinds"`
	Providers []requeststore.ProviderTrace `json:"providers"`           // mapped results per provider
	Exchanges []requeststore.Exchange      `json:"exchanges"`           // raw provider JSON
	Merged    []requeststore.ResultRow     `json:"merged"`              // after entity-resolution merge
	Ranked    []requeststore.ResultRow     `json:"ranked"`              // after rank, before reshape
	Final     []requeststore.ResultRow     `json:"final"`               // after diversity + collapse
	TookMs    int64                        `json:"took_ms"`
}

// ReRunner runs a fresh discovery search through a recording client and returns
// the stage-by-stage waterfall. Satisfied at the composition root (it needs the
// provider wiring); the admin handler only consumes it.
type ReRunner interface {
	ReRun(ctx context.Context, query string, kinds []string) (ReRunResult, error)
}

// WithReRunner attaches the on-demand re-run inspector. A nil runner disables the
// endpoint (503).
func (h *AdminHandler) WithReRunner(r ReRunner) *AdminHandler {
	h.reRunner = r
	return h
}

type reRunRequest struct {
	Query string   `json:"query"`
	Kinds []string `json:"kinds"`
}

// serveReRun runs an operator-supplied query live through the discovery pipeline
// and returns the full waterfall. Live: hits provider APIs, bypasses the live
// circuit breakers, shares live keys.
func (h *AdminHandler) serveReRun(w http.ResponseWriter, r *http.Request) {
	if h.reRunner == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "re-run inspector not configured")
		return
	}
	var body reRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	result, err := h.reRunner.ReRun(r.Context(), body.Query, body.Kinds)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

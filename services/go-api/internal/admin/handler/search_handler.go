package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
)

// SearchInspector runs an operator-supplied query live through the REAL discovery
// pipeline — including artwork + durable-identity resolution — and returns the
// ranked results with how each one's artwork resolved. It bypasses the app-wide
// result cache so every call is live. Satisfied at the composition root (it holds
// the search service); the admin handler only consumes it.
type SearchInspector interface {
	InspectSearch(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error)
}

// WithSearchInspector attaches the Mission Control test-search. A nil inspector
// disables the endpoint (503).
func (h *AdminHandler) WithSearchInspector(s SearchInspector) *AdminHandler {
	h.searchInspector = s
	return h
}

type testSearchRequest struct {
	Query string   `json:"query"`
	Kinds []string `json:"kinds"`
}

type testSearchResponse struct {
	Query   string                   `json:"query"`
	Results []requeststore.ResultRow `json:"results"`
}

// serveTestSearch runs an operator query live and returns the ranked, enriched
// results (artwork URL + source + resolution path per result). Lets the operator
// test discovery — including the durable-identity artwork fix — entirely from the
// console, with no app or curl needed.
func (h *AdminHandler) serveTestSearch(w http.ResponseWriter, r *http.Request) {
	if h.searchInspector == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "test search not configured")
		return
	}
	var body testSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	results, err := h.searchInspector.InspectSearch(r.Context(), body.Query, body.Kinds)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, testSearchResponse{Query: body.Query, Results: results})
}

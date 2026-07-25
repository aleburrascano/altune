package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
)

type SearchInspector interface {
	InspectSearch(ctx context.Context, query string, kinds []string) ([]requeststore.ResultRow, error)
}

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

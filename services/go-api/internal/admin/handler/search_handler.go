package handler

import (
	"context"
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

type testSearchResponse struct {
	Query   string                   `json:"query"`
	Results []requeststore.ResultRow `json:"results"`
}

func (h *AdminHandler) serveTestSearch(w http.ResponseWriter, r *http.Request) {
	if h.searchInspector == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "test search not configured")
		return
	}
	body, ok := decodeQuery(w, r)
	if !ok {
		return
	}
	results, err := h.searchInspector.InspectSearch(r.Context(), body.Query, body.Kinds)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, testSearchResponse{Query: body.Query, Results: results})
}

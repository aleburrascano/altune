package handler

import (
	"context"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
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
	h.serveQueryAction(w, r, h.searchInspector != nil, errSearchUnavailable, "admin.test_search_failed",
		func(ctx context.Context, body queryRequest) (any, error) {
			results, err := h.searchInspector.InspectSearch(ctx, body.Query, body.Kinds)
			if err != nil {
				return nil, err
			}
			return testSearchResponse{Query: body.Query, Results: results}, nil
		})
}

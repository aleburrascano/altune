package handler

import (
	"context"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
)

type ReRunResult struct {
	Query     string                       `json:"query"`
	Kinds     []string                     `json:"kinds"`
	Providers []requeststore.ProviderTrace `json:"providers"`
	Exchanges []requeststore.Exchange      `json:"exchanges"`
	Merged    []requeststore.ResultRow     `json:"merged"`
	RankTrace []ScoredRow                  `json:"rank_trace"`
	Final     []requeststore.ResultRow     `json:"final"`
	TookMs    int64                        `json:"took_ms"`
}

type ScoredRow struct {
	requeststore.ResultRow
	Relevance   float64 `json:"relevance"`
	Prominence  float64 `json:"prominence"`
	Behavioral  float64 `json:"behavioral"`
	Popularity  float64 `json:"popularity"`
	RRF         float64 `json:"rrf"`
	MultiSource bool    `json:"multi_source"`
	Demoted     bool    `json:"demoted"`
}

type ReRunner interface {
	ReRun(ctx context.Context, query string, kinds []string) (ReRunResult, error)
}

func (h *AdminHandler) WithReRunner(r ReRunner) *AdminHandler {
	h.reRunner = r
	return h
}

func (h *AdminHandler) serveReRun(w http.ResponseWriter, r *http.Request) {
	if h.reRunner == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "re-run inspector not configured")
		return
	}
	body, ok := decodeQuery(w, r)
	if !ok {
		return
	}
	result, err := h.reRunner.ReRun(r.Context(), body.Query, body.Kinds)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

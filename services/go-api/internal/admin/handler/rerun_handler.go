package handler

import (
	"context"
	"net/http"

	"altune/go-api/internal/admin/requeststore"
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
	h.serveQueryAction(w, r, h.reRunner != nil, errReRunUnavailable, "admin.rerun_failed",
		func(ctx context.Context, body queryRequest) (any, error) {
			return h.reRunner.ReRun(ctx, body.Query, body.Kinds)
		})
}

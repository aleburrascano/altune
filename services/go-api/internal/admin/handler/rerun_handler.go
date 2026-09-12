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

type ReRunner func(ctx context.Context, query string, kinds []string) (requeststore.ReRunResult, error)

func (h *AdminHandler) WithReRunner(r ReRunner) *AdminHandler {
	h.reRunner = r
	return h
}

func (h *AdminHandler) serveReRun(w http.ResponseWriter, r *http.Request) {
	h.serveQueryAction(w, r, h.reRunner != nil, errReRunUnavailable, "admin.rerun_failed",
		func(ctx context.Context, body queryRequest) (any, error) {
			res, err := h.reRunner(ctx, body.Query, body.Kinds)
			if err != nil {
				return nil, err
			}
			return reRunResultDTO(res), nil
		})
}

// reRunResultDTO maps the orchestration-owned rerun result into this package's
// json-tagged wire DTO, field-for-field, so the endpoint's response shape is
// owned here at the admin boundary rather than by internal/app.
func reRunResultDTO(r requeststore.ReRunResult) ReRunResult {
	return ReRunResult{
		Query:     r.Query,
		Kinds:     r.Kinds,
		Providers: r.Providers,
		Exchanges: r.Exchanges,
		Merged:    r.Merged,
		RankTrace: scoredRowsDTO(r.RankTrace),
		Final:     r.Final,
		TookMs:    r.TookMs,
	}
}

func scoredRowsDTO(rows []requeststore.ScoredRow) []ScoredRow {
	if rows == nil {
		return nil
	}
	out := make([]ScoredRow, len(rows))
	for i, s := range rows {
		out[i] = ScoredRow{
			ResultRow:   s.ResultRow,
			Relevance:   s.Relevance,
			Prominence:  s.Prominence,
			Behavioral:  s.Behavioral,
			Popularity:  s.Popularity,
			RRF:         s.RRF,
			MultiSource: s.MultiSource,
			Demoted:     s.Demoted,
		}
	}
	return out
}

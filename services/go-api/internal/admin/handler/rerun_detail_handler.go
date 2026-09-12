package handler

import (
	"context"
	"net/http"
)

type DetailReRunResult struct {
	Query      string            `json:"query"`
	Resolved   *DetailEntity     `json:"resolved"`
	AlbumSeeds []DetailSeedGroup `json:"album_seeds"`
	TrackSeeds []DetailSeedGroup `json:"track_seeds"`
	Albums     []DetailItemRow   `json:"albums"`
	TopTracks  []DetailItemRow   `json:"top_tracks"`
	TookMs     int64             `json:"took_ms"`
}

type DetailEntity struct {
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle"`
	MBID     string            `json:"mbid"`
	Sources  map[string]string `json:"sources"`
}

type DetailSeedGroup struct {
	Provider   string          `json:"provider"`
	ExternalID string          `json:"external_id"`
	Status     string          `json:"status"`
	Error      string          `json:"error,omitempty"`
	Items      []DetailItemRow `json:"items"`
}

type DetailItemRow struct {
	Title      string   `json:"title"`
	Subtitle   string   `json:"subtitle"`
	Year       int      `json:"year"`
	TrackCount int      `json:"track_count"`
	RecordType string   `json:"record_type"`
	ImageURL   string   `json:"image_url"`
	Sources    []string `json:"sources"`
}

type DetailReRunner interface {
	ReRunDetail(ctx context.Context, query string) (DetailReRunResult, error)
}

func (h *AdminHandler) WithDetailReRunner(r DetailReRunner) *AdminHandler {
	h.detailReRunner = r
	return h
}

func (h *AdminHandler) serveReRunDetail(w http.ResponseWriter, r *http.Request) {
	h.serveQueryAction(w, r, h.detailReRunner != nil, errDetailUnavailable, "admin.rerun_detail_failed",
		func(ctx context.Context, body queryRequest) (any, error) {
			return h.detailReRunner.ReRunDetail(ctx, body.Query)
		})
}

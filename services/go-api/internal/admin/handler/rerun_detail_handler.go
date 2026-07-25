package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/shared/httputil"
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

type detailReRunRequest struct {
	Query string `json:"query"`
}

func (h *AdminHandler) serveReRunDetail(w http.ResponseWriter, r *http.Request) {
	if h.detailReRunner == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "detail re-run inspector not configured")
		return
	}
	var body detailReRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	result, err := h.detailReRunner.ReRunDetail(r.Context(), body.Query)
	if err != nil {
		httputil.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// DetailReRunResult is the phone-faithful artist-detail waterfall: the artist
// entity search resolved (the seed ids the app opens), each seed's isolated
// content fan-out, and the client-merged discography + top tracks — so the
// operator sees exactly what the app renders on an artist screen. The gap the
// search-only /rerun can't show, because detail is a separate pipeline.
type DetailReRunResult struct {
	Query      string            `json:"query"`
	Resolved   *DetailEntity     `json:"resolved"` // nil when no artist resolved
	AlbumSeeds []DetailSeedGroup `json:"album_seeds"`
	TrackSeeds []DetailSeedGroup `json:"track_seeds"`
	Albums     []DetailItemRow   `json:"albums"`     // client-merged discography (what renders)
	TopTracks  []DetailItemRow   `json:"top_tracks"` // client-merged top tracks (what renders)
	TookMs     int64             `json:"took_ms"`
}

// DetailEntity is the resolved artist plus the per-provider seed ids the client
// fans out to (provider → external id) and the MBID the Last.fm top-tracks key on.
type DetailEntity struct {
	Title    string            `json:"title"`
	Subtitle string            `json:"subtitle"`
	MBID     string            `json:"mbid"`
	Sources  map[string]string `json:"sources"`
}

// DetailSeedGroup is one seed provider's isolated content response — kept
// un-merged so contamination is attributable to the exact seed that carried it.
type DetailSeedGroup struct {
	Provider   string          `json:"provider"`
	ExternalID string          `json:"external_id"`
	Status     string          `json:"status"`
	Items      []DetailItemRow `json:"items"`
}

// DetailItemRow is one discography/top-track row with the fields the console
// shows: title, the release metadata, and the providers that contributed it.
type DetailItemRow struct {
	Title      string   `json:"title"`
	Subtitle   string   `json:"subtitle"`
	Year       int      `json:"year"`
	TrackCount int      `json:"track_count"`
	RecordType string   `json:"record_type"`
	ImageURL   string   `json:"image_url"`
	Sources    []string `json:"sources"`
}

// DetailReRunner reproduces the mobile client's artist-detail fan-out for a query:
// resolve the top artist entity through the real search service, then run the same
// per-seed content calls useArtistContent makes and the same client-side merge.
// Satisfied at the composition root (it holds the live search + content services).
type DetailReRunner interface {
	ReRunDetail(ctx context.Context, query string) (DetailReRunResult, error)
}

// WithDetailReRunner attaches the phone-faithful artist-detail re-run. A nil
// runner disables the endpoint (503).
func (h *AdminHandler) WithDetailReRunner(r DetailReRunner) *AdminHandler {
	h.detailReRunner = r
	return h
}

type detailReRunRequest struct {
	Query string `json:"query"`
}

// serveReRunDetail resolves an artist query and reproduces the app's detail-screen
// fan-out + merge, live. Same provider APIs the app hits; operator-gated.
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

package handler

import (
	"altune/go-api/internal/admin/requeststore"
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

type DetailReRunner func(ctx context.Context, query string) (requeststore.DetailReRunResult, error)

func (h *AdminHandler) WithDetailReRunner(r DetailReRunner) *AdminHandler {
	h.detailReRunner = r
	return h
}

func (h *AdminHandler) serveReRunDetail(w http.ResponseWriter, r *http.Request) {
	h.serveQueryAction(w, r, h.detailReRunner != nil, errDetailUnavailable, "admin.rerun_detail_failed",
		func(ctx context.Context, body queryRequest) (any, error) {
			res, err := h.detailReRunner(ctx, body.Query)
			if err != nil {
				return nil, err
			}
			return detailReRunResultDTO(res), nil
		})
}

// detailReRunResultDTO maps the orchestration-owned detail rerun result into
// this package's json-tagged wire DTO. Nil slices and a nil resolved entity are
// preserved so the response stays byte-for-byte identical (null, not []).
func detailReRunResultDTO(r requeststore.DetailReRunResult) DetailReRunResult {
	return DetailReRunResult{
		Query:      r.Query,
		Resolved:   detailEntityDTO(r.Resolved),
		AlbumSeeds: detailSeedGroupsDTO(r.AlbumSeeds),
		TrackSeeds: detailSeedGroupsDTO(r.TrackSeeds),
		Albums:     detailItemRowsDTO(r.Albums),
		TopTracks:  detailItemRowsDTO(r.TopTracks),
		TookMs:     r.TookMs,
	}
}

func detailEntityDTO(e *requeststore.DetailEntity) *DetailEntity {
	if e == nil {
		return nil
	}
	return &DetailEntity{
		Title:    e.Title,
		Subtitle: e.Subtitle,
		MBID:     e.MBID,
		Sources:  e.Sources,
	}
}

func detailSeedGroupsDTO(groups []requeststore.DetailSeedGroup) []DetailSeedGroup {
	if groups == nil {
		return nil
	}
	out := make([]DetailSeedGroup, len(groups))
	for i, g := range groups {
		out[i] = DetailSeedGroup{
			Provider:   g.Provider,
			ExternalID: g.ExternalID,
			Status:     g.Status,
			Error:      g.Error,
			Items:      detailItemRowsDTO(g.Items),
		}
	}
	return out
}

func detailItemRowsDTO(items []requeststore.DetailItemRow) []DetailItemRow {
	if items == nil {
		return nil
	}
	out := make([]DetailItemRow, len(items))
	for i, it := range items {
		out[i] = DetailItemRow{
			Title:      it.Title,
			Subtitle:   it.Subtitle,
			Year:       it.Year,
			TrackCount: it.TrackCount,
			RecordType: it.RecordType,
			ImageURL:   it.ImageURL,
			Sources:    it.Sources,
		}
	}
	return out
}

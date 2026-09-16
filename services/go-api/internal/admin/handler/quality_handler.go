package handler

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// defaultQualityWindowDays is the window the discography-quality endpoint reads
// when window_days is absent, matching the pinned seam (?window_days=30).
const defaultQualityWindowDays = 30

// maxQualityWindowDays caps the requested window so a hostile or fat-fingered
// window_days cannot force an unbounded scan of discovery_events.
const maxQualityWindowDays = 365

// qualityLatestN bounds how many latest observations T1 returns. Worst-first
// ordering and grouping are a later slice; this only caps the response size.
const qualityLatestN = 200

// defaultQualityTimeout bounds the discography-quality query so a stalled DB
// cannot park an /admin/quality/discography request (and its pooled connection).
const defaultQualityTimeout = 5 * time.Second

// discographyGroupByArtist is the only grouping T1 serves; the seam reserves the
// by= param for later slices (by provider, by contamination band).
const discographyGroupByArtist = "artist"

// WithDiscographyQuality supplies the read-only discography structural-quality
// source. Absent it, the endpoint answers an empty case list rather than 500, so
// the operator surface degrades instead of failing.
func (h *AdminHandler) WithDiscographyQuality(r ports.DiscographyQualityReader) *AdminHandler {
	h.discographyQuality = r
	return h
}

// discographyCaseDTO is one artist's structural-quality case on the wire. artist
// is the artist_ref for T1 (the tracer records no display name); later slices
// resolve a human name. Every field is data the Overseer must HTML-escape before
// render.
type discographyCaseDTO struct {
	Artist         string         `json:"artist"`
	ArtistRef      string         `json:"artist_ref"`
	Releases       int            `json:"releases"`
	SingleProvider int            `json:"single_provider"`
	ProviderCounts map[string]int `json:"provider_counts"`
	LastSeen       time.Time      `json:"last_seen"`
}

type discographyQualityResponse struct {
	WindowDays int                  `json:"window_days"`
	GroupBy    string               `json:"group_by"`
	Cases      []discographyCaseDTO `json:"cases"`
}

// serveDiscographyQuality answers GET /admin/quality/discography (operator-only,
// mounted under the operator gate). It reads the latest structural-quality cases
// inside the requested window and returns the pinned JSON. It is a pure read: it
// serves the verdict go-api already computed at the merge, never recomputing it.
func (h *AdminHandler) serveDiscographyQuality(w http.ResponseWriter, r *http.Request) {
	windowDays := clampWindowDays(r.URL.Query().Get("window_days"))
	resp := discographyQualityResponse{
		WindowDays: windowDays,
		GroupBy:    discographyGroupByArtist,
		Cases:      []discographyCaseDTO{},
	}
	if h.discographyQuality == nil {
		httputil.WriteJSON(w, http.StatusOK, resp)
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	cases, err := h.queryDiscographyQuality(r.Context(), since)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	resp.Cases = toDiscographyCaseDTOs(cases)
	httputil.WriteJSON(w, http.StatusOK, resp)
}

var errDiscographyQualityTimeout = &codedError{
	msg:    "discography quality query timed out",
	status: http.StatusGatewayTimeout,
	code:   "admin.discography_quality_timeout",
}

// queryDiscographyQuality runs the read under a bounded timeout derived from the
// request context, so a stalled query surfaces as a coded 504 rather than parking
// the request or its pooled connection.
func (h *AdminHandler) queryDiscographyQuality(ctx context.Context, since time.Time) ([]ports.DiscographyCase, error) {
	queryCtx, cancel := context.WithTimeout(ctx, defaultQualityTimeout)
	defer cancel()
	cases, err := h.discographyQuality.DiscographyQuality(queryCtx, since, qualityLatestN)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, errDiscographyQualityTimeout
	}
	return cases, err
}

// clampWindowDays parses window_days and clamps it to [1, maxQualityWindowDays],
// defaulting on an absent, non-numeric or non-positive value. Clamping (never
// echoing the raw string) is what keeps a hostile window_days from forcing an
// unbounded scan or reflecting attacker input into the response.
func clampWindowDays(raw string) int {
	if raw == "" {
		return defaultQualityWindowDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultQualityWindowDays
	}
	if n > maxQualityWindowDays {
		return maxQualityWindowDays
	}
	return n
}

// toDiscographyCaseDTOs maps the domain cases to the wire shape, defaulting a nil
// provider map to an empty object so the JSON is always well-formed.
func toDiscographyCaseDTOs(cases []ports.DiscographyCase) []discographyCaseDTO {
	out := make([]discographyCaseDTO, 0, len(cases))
	for _, c := range cases {
		counts := c.ProviderCounts
		if counts == nil {
			counts = map[string]int{}
		}
		out = append(out, discographyCaseDTO{
			Artist:         c.ArtistRef,
			ArtistRef:      c.ArtistRef,
			Releases:       c.Releases,
			SingleProvider: c.SingleProvider,
			ProviderCounts: counts,
			LastSeen:       c.LastSeen.UTC(),
		})
	}
	return out
}

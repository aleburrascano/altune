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

// qualityLatestN caps how many worst-first cases the endpoint returns, bounding
// the response after the reader has ranked and grouped the windowed observations.
const qualityLatestN = 200

// defaultQualityTimeout bounds the discography-quality query so a stalled DB
// cannot park an /admin/quality/discography request (and its pooled connection).
const defaultQualityTimeout = 5 * time.Second

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
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

type discographyQualityResponse struct {
	WindowDays int                  `json:"window_days"`
	GroupBy    string               `json:"group_by"`
	Cases      []discographyCaseDTO `json:"cases"`
	// SuspectRate is the windowed headline in [0,1]: the share of real discography
	// opens whose top release-suspect fired. LastSampleAt is the most recent open's
	// time (zero when the window held none), so the Overseer can show the headline's
	// freshness. Both are computed over server-emitted opens only — eval/synthetic
	// traffic emits none — so an eval run can never move the rate.
	SuspectRate  float64   `json:"suspect_rate"`
	LastSampleAt time.Time `json:"last_sample_at"`
}

// serveDiscographyQuality answers GET /admin/quality/discography (operator-only,
// mounted under the operator gate). It reads the worst-first structural-quality
// cases inside the requested window, grouped by the by= param, and returns the
// pinned JSON. It is a pure read: it serves the verdict go-api already computed at
// the merge, never recomputing it.
func (h *AdminHandler) serveDiscographyQuality(w http.ResponseWriter, r *http.Request) {
	windowDays := clampWindowDays(r.URL.Query().Get("window_days"))
	groupBy := ports.ParseDiscographyGroupBy(r.URL.Query().Get("by"))
	resp := discographyQualityResponse{
		WindowDays: windowDays,
		GroupBy:    string(groupBy),
		Cases:      []discographyCaseDTO{},
	}
	if h.discographyQuality == nil {
		httputil.WriteJSON(w, http.StatusOK, resp)
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	cases, suspect, err := h.queryDiscographyQuality(r.Context(), since, groupBy)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	resp.Cases = toDiscographyCaseDTOs(cases)
	resp.SuspectRate = suspect.Rate
	resp.LastSampleAt = suspect.LastSample.UTC()
	httputil.WriteJSON(w, http.StatusOK, resp)
}

var errDiscographyQualityTimeout = &codedError{
	msg:    "discography quality query timed out",
	status: http.StatusGatewayTimeout,
	code:   "admin.discography_quality_timeout",
}

// queryDiscographyQuality runs the worst-first read and the windowed suspect-rate
// read under one bounded timeout derived from the request context, so a stalled
// query surfaces as a coded 504 rather than parking the request or its pooled
// connection. Both reads share the same window, so the served headline rate and
// the served cases describe the same slice of opens.
func (h *AdminHandler) queryDiscographyQuality(ctx context.Context, since time.Time, groupBy ports.DiscographyGroupBy) ([]ports.DiscographyCase, ports.DiscographySuspectRate, error) {
	queryCtx, cancel := context.WithTimeout(ctx, defaultQualityTimeout)
	defer cancel()
	cases, err := h.discographyQuality.DiscographyQuality(queryCtx, since, groupBy, qualityLatestN)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ports.DiscographySuspectRate{}, errDiscographyQualityTimeout
	}
	if err != nil {
		return nil, ports.DiscographySuspectRate{}, err
	}
	suspect, err := h.discographyQuality.SuspectRate(queryCtx, since)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ports.DiscographySuspectRate{}, errDiscographyQualityTimeout
	}
	return cases, suspect, err
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
			Artist:             c.ArtistRef,
			ArtistRef:          c.ArtistRef,
			Releases:           c.Releases,
			SingleProvider:     c.SingleProvider,
			SingleProviderNoID: c.SingleProviderNoID,
			ProviderCounts:     counts,
			LastSeen:           c.LastSeen.UTC(),
		})
	}
	return out
}

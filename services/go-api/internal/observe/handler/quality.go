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

const defaultQualityWindowDays = 30

const maxQualityWindowDays = 365

const qualityLatestN = 200

const defaultQualityTimeout = 5 * time.Second

var errDiscographyQualityTimeout = &codedError{
	msg:    "discography quality query timed out",
	status: http.StatusGatewayTimeout,
	code:   "observe.discography_quality_timeout",
}

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
	WindowDays   int                  `json:"window_days"`
	GroupBy      string               `json:"group_by"`
	Cases        []discographyCaseDTO `json:"cases"`
	SuspectRate  float64              `json:"suspect_rate"`
	LastSampleAt time.Time            `json:"last_sample_at"`
}

func emptyDiscographyQualityResponse(windowDays int, groupBy ports.DiscographyGroupBy) discographyQualityResponse {
	return discographyQualityResponse{
		WindowDays: windowDays,
		GroupBy:    string(groupBy),
		Cases:      []discographyCaseDTO{},
	}
}

func (h *Handler) serveDiscographyQuality(w http.ResponseWriter, r *http.Request) {
	windowDays := clampWindowDays(r.URL.Query().Get("window_days"))
	groupBy := ports.ParseDiscographyGroupBy(r.URL.Query().Get("by"))
	resp := emptyDiscographyQualityResponse(windowDays, groupBy)
	if h.deps.Discography == nil {
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

func (h *Handler) queryDiscographyQuality(ctx context.Context, since time.Time, groupBy ports.DiscographyGroupBy) ([]ports.DiscographyCase, ports.DiscographySuspectRate, error) {
	queryCtx, cancel := context.WithTimeout(ctx, defaultQualityTimeout)
	defer cancel()
	cases, err := h.deps.Discography.DiscographyQuality(queryCtx, since, groupBy, qualityLatestN)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ports.DiscographySuspectRate{}, errDiscographyQualityTimeout
	}
	if err != nil {
		return nil, ports.DiscographySuspectRate{}, err
	}
	suspect, err := h.deps.Discography.SuspectRate(queryCtx, since)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ports.DiscographySuspectRate{}, errDiscographyQualityTimeout
	}
	return cases, suspect, err
}

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

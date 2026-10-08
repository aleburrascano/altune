package handler

import (
	"altune/go-api/internal/observe/adapters/discoverybridge"
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"strconv"
	"time"
)

const defaultQualityWindowDays = 30

const maxQualityWindowDays = 365

const qualityLatestN = 200

const defaultQualityTimeout = 5 * time.Second

type discographyQualityResponse struct {
	WindowDays   int                                  `json:"window_days"`
	GroupBy      string                               `json:"group_by"`
	Cases        []discoverybridge.DiscographyCaseDTO `json:"cases"`
	SuspectRate  float64                              `json:"suspect_rate"`
	LastSampleAt time.Time                            `json:"last_sample_at"`
}

func emptyDiscographyQualityResponse(windowDays int, groupBy discoverybridge.DiscographyGroupBy) discographyQualityResponse {
	return discographyQualityResponse{
		WindowDays: windowDays,
		GroupBy:    string(groupBy),
		Cases:      []discoverybridge.DiscographyCaseDTO{},
	}
}

func (h *Handler) serveDiscographyQuality(w http.ResponseWriter, r *http.Request) {
	windowDays := clampWindowDays(r.URL.Query().Get("window_days"))
	groupBy := discoverybridge.ParseDiscographyGroupBy(r.URL.Query().Get("by"))
	resp := emptyDiscographyQualityResponse(windowDays, groupBy)
	if h.deps.Discography == nil {
		httputil.WriteJSON(w, http.StatusOK, resp)
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -windowDays)
	queryCtx, cancel := context.WithTimeout(r.Context(), defaultQualityTimeout)
	defer cancel()
	cases, suspectRate, lastSample, err := discoverybridge.QueryDiscographyQuality(queryCtx, h.deps.Discography, since, groupBy, qualityLatestN)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	resp.Cases = cases
	resp.SuspectRate = suspectRate
	resp.LastSampleAt = lastSample
	httputil.WriteJSON(w, http.StatusOK, resp)
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

package handler

import (
	"altune/go-api/internal/observe/internal/core/ports"
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
	queryCtx, cancel := context.WithTimeout(r.Context(), defaultQualityTimeout)
	defer cancel()
	result, err := queryDiscographyQuality(queryCtx, h.deps.Discography, since, groupBy, qualityLatestN)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	resp.Cases = result.cases
	resp.SuspectRate = result.suspectRate
	resp.LastSampleAt = result.lastSample
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

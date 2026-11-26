package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
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
	windowDays, err := parseWindowDays(r.URL.Query().Get("window_days"))
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	groupBy, err := groupByFromQuery(r.URL.Query().Get("by"))
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	resp := emptyDiscographyQualityResponse(windowDays, groupBy)
	if h.deps.Discography == nil {
		httpserver.WriteJSON(w, http.StatusOK, resp)
		return
	}
	since := h.now.UTC().AddDate(0, 0, -windowDays)
	queryCtx, cancel := context.WithTimeout(r.Context(), defaultQualityTimeout)
	defer cancel()
	result, err := queryDiscographyQuality(queryCtx, h.deps.Discography, since, groupBy, qualityLatestN)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	resp.Cases = result.cases
	resp.SuspectRate = result.suspectRate
	resp.LastSampleAt = result.lastSample
	httpserver.WriteJSON(w, http.StatusOK, resp)
}

func groupByFromQuery(raw string) (ports.DiscographyGroupBy, error) {
	if raw == "" {
		return ports.GroupByArtist, nil
	}
	groupBy, err := ports.ParseDiscographyGroupBy(raw)
	if err != nil {
		return "", invalidGroupBy(err)
	}
	return groupBy, nil
}

func parseWindowDays(raw string) (int, error) {
	if raw == "" {
		return defaultQualityWindowDays, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, apierror.NewError(apierror.CodeObserveInvalidWindowDays, "window_days must be a positive integer")
	}
	return min(n, maxQualityWindowDays), nil
}

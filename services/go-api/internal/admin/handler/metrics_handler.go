package handler

import (
	"context"
	"net/http"
	"strconv"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
)

const defaultMetricsHistoryDays = 30

type MetricsHistoryReader interface {
	MetricsHistory(ctx context.Context, metric string, days int) ([]ports.MetricPoint, error)
}

func (h *AdminHandler) WithMetricsHistory(m MetricsHistoryReader) *AdminHandler {
	h.metricsHistory = m
	return h
}

func (h *AdminHandler) serveMetricsHistory(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		httputil.WriteError(w, http.StatusBadRequest, "metric query param is required")
		return
	}
	if h.metricsHistory == nil {
		httputil.WriteJSON(w, http.StatusOK, []ports.MetricPoint{})
		return
	}
	days := defaultMetricsHistoryDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			days = n
		}
	}
	points, err := h.metricsHistory.MetricsHistory(r.Context(), metric, days)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, points)
}

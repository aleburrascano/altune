package handler

import (
	"context"
	"net/http"
	"strconv"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
)

// defaultMetricsHistoryDays bounds the response when the client omits ?days.
const defaultMetricsHistoryDays = 30

// MetricsHistoryReader is the read side of the discovery_metrics rollup,
// satisfied by the Postgres adapter. Defined here (where consumed) so the
// admin handler depends on a narrow interface, not the persistence type.
type MetricsHistoryReader interface {
	MetricsHistory(ctx context.Context, metric string, days int) ([]ports.MetricPoint, error)
}

// WithMetricsHistory attaches the durable discovery_metrics rollup reader —
// the only history that survives a restart. Nil leaves the panel empty.
func (h *AdminHandler) WithMetricsHistory(m MetricsHistoryReader) *AdminHandler {
	h.metricsHistory = m
	return h
}

// serveMetricsHistory returns the last N days of a rolled-up metric
// (zero_result_rate, ctr, tail_noise_top5_avg, searches) — the only durable,
// restart-surviving history Mission Control has. Nil-tolerant: without a
// reader the panel answers empty rather than the console failing to load.
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

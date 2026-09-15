package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
)

const defaultMetricsHistoryDays = 30

// defaultMetricsHistoryTimeout bounds the metrics-history query so a stalled DB
// cannot park an /admin/metrics request (and its pooled connection) forever.
const defaultMetricsHistoryTimeout = 5 * time.Second

func (h *AdminHandler) WithMetricsHistory(m ports.MetricsRollupStore) *AdminHandler {
	h.metricsHistory = m
	return h
}

func (h *AdminHandler) serveMetricsHistory(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		httputil.HandleServiceError(w, r, errMetricRequired)
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
	points, err := h.queryMetricsHistory(r.Context(), metric, days)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, points)
}

var errMetricsHistoryTimeout = &codedError{
	msg:    "metrics history query timed out",
	status: http.StatusGatewayTimeout,
	code:   "admin.metrics_history_timeout",
}

// queryMetricsHistory invokes the store under a bounded timeout derived from the
// request context (mirroring runProbe). An expired deadline surfaces as a coded
// 504 so a stalled query cannot park the request or its pooled connection.
func (h *AdminHandler) queryMetricsHistory(ctx context.Context, metric string, days int) ([]ports.MetricPoint, error) {
	timeout := h.metricsHistoryTimeout
	if timeout <= 0 {
		timeout = defaultMetricsHistoryTimeout
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	points, err := h.metricsHistory.MetricsHistory(queryCtx, metric, days)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, errMetricsHistoryTimeout
	}
	return points, err
}

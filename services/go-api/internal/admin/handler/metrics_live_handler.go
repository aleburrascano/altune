package handler

import (
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
)

// liveMetrics is the operator-facing view of the live in-process counters. It
// aggregates only the explicitly named per-module expvar counters — never the
// raw expvar registry, which also publishes process globals (cmdline, memstats) —
// plus the per-route request-latency histogram.
type liveMetrics struct {
	Auth     authmetrics.Snapshot     `json:"auth"`
	Catalog  catalogmetrics.Snapshot  `json:"catalog"`
	Feedback feedbackmetrics.Snapshot `json:"feedback"`
	Latency  reqmetrics.Snapshot      `json:"latency"`
}

// serveMetricsLive returns the current auth, catalog and feedback expvar counters
// and the per-route latency histogram as one typed JSON response. It is
// registered behind OperatorOnly, so raw expvar counters are never
// world-readable and no /debug/vars handler is mounted.
func (h *AdminHandler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, liveMetrics{
		Auth:     authmetrics.ReadSnapshot(),
		Catalog:  catalogmetrics.ReadSnapshot(),
		Feedback: feedbackmetrics.ReadSnapshot(),
		Latency:  reqmetrics.ReadSnapshot(),
	})
}

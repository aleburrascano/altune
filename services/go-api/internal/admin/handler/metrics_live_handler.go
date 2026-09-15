package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"

	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
)

// liveMetrics is the operator-facing view of the live in-process counters. It
// aggregates only the explicitly named per-module expvar counters — never the
// raw expvar registry, which also publishes process globals (cmdline, memstats).
type liveMetrics struct {
	Catalog  catalogmetrics.Snapshot  `json:"catalog"`
	Feedback feedbackmetrics.Snapshot `json:"feedback"`
}

// serveMetricsLive returns the current catalog and feedback expvar counters as
// one typed JSON response. It is registered behind OperatorOnly, so raw expvar
// counters are never world-readable and no /debug/vars handler is mounted.
func (h *AdminHandler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, liveMetrics{
		Catalog:  catalogmetrics.ReadSnapshot(),
		Feedback: feedbackmetrics.ReadSnapshot(),
	})
}

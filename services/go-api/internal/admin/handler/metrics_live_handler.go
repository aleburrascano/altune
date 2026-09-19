package handler

import (
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"
)

// liveMetrics is the operator-facing view of the live in-process counters. It
// aggregates only the explicitly named per-module expvar counters — never the
// raw expvar registry, which also publishes process globals (cmdline, memstats) —
// plus the per-route request-latency histogram and its 2xx/4xx/5xx status counts.
type liveMetrics struct {
	Auth      authmetrics.Snapshot     `json:"auth"`
	Catalog   catalogmetrics.Snapshot  `json:"catalog"`
	Feedback  feedbackmetrics.Snapshot `json:"feedback"`
	Playback  playbackmetrics.Snapshot `json:"playback"`
	Providers providermetrics.Snapshot `json:"providers"`
	Latency   reqmetrics.Snapshot      `json:"latency"`
}

// serveMetricsLive returns the current auth, catalog, feedback and playback
// expvar counters and the per-route latency histogram as one typed JSON
// response. It is registered behind OperatorOnly, so raw expvar counters are
// never world-readable and no /debug/vars handler is mounted.
func (h *AdminHandler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, liveMetrics{
		Auth:      authmetrics.ReadSnapshot(),
		Catalog:   catalogmetrics.ReadSnapshot(),
		Feedback:  feedbackmetrics.ReadSnapshot(),
		Playback:  playbackmetrics.ReadSnapshot(),
		Providers: providermetrics.ReadSnapshot(),
		Latency:   reqmetrics.ReadSnapshot(),
	})
}

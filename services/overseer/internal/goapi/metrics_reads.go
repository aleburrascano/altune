package goapi

import "context"

// adminMetricsLivePath is go-api's operator live-metrics endpoint. Like
// /admin/health it is mounted under the admin-guarded "/admin" group
// (internal/app/admin_wiring.go), so the request must carry the read-only bearer
// the client already attaches. It exposes the in-process counters plus the
// per-route request-latency histogram (internal/admin/handler/metrics_live_handler.go).
const adminMetricsLivePath = "/admin/metrics/live"

// LiveMetrics mirrors the subset of go-api's GET /admin/metrics/live the
// Back-end performance bucket consumes: the per-route latency histogram. The
// endpoint also returns per-module counters (auth/catalog/feedback/playback);
// those fields are intentionally omitted here, and json ignores them, so this
// read stays a focused latency mirror and tolerates go-api adding counters
// without a decode failure.
type LiveMetrics struct {
	Latency LatencyMetrics `json:"latency"`
}

// LatencyMetrics is the per-route latency histogram from the live-metrics
// snapshot, keyed by chi route template (e.g. "/v1/tracks/{trackId}"). The
// template — never a raw path — keeps route cardinality bounded on go-api's side.
type LatencyMetrics struct {
	Routes map[string]RouteLatency `json:"routes"`
}

// RouteLatency is one route's latency distribution: total request count, the sum
// of observed latencies (ms), and the fixed histogram buckets. It mirrors go-api's
// reqmetrics.RouteLatency shape; percentiles are estimated from Buckets by the
// bucket, not computed here — this stays a pure read.
type RouteLatency struct {
	Count   uint64          `json:"count"`
	SumMs   uint64          `json:"sum_ms"`
	Buckets []LatencyBucket `json:"buckets"`
}

// LatencyBucket is one histogram bucket: the count of requests at or below LeMs
// milliseconds. Counts are per-bucket (not cumulative). The final bucket carries
// the label "+Inf" and holds everything slower than the last fixed bound, so it
// has no finite upper bound.
type LatencyBucket struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

// AdminMetricsLive fetches GET /admin/metrics/live and decodes its per-route
// latency histogram. It reuses the read primitive, so the read-only bearer, the
// host pin, the bounded body and the timeout all apply: an unreachable go-api
// yields a SourceDownError, a rejected token or a principal the admin gate refuses yields an
// APIError, and a runaway body cannot exhaust memory. It is a read; nothing here
// writes, commands or mutates go-api.
func (c *Client) AdminMetricsLive(ctx context.Context) (LiveMetrics, error) {
	var out LiveMetrics
	if err := c.get(ctx, adminMetricsLivePath, &out); err != nil {
		return LiveMetrics{}, err
	}
	return out, nil
}

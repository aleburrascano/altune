package goapi

import "context"

// observeMetricsLivePath is go-api's operator live-metrics endpoint. Like
// /observe/health it is mounted under the observe-guarded "/observe" group
// (internal/app/observe_wiring.go), so the request must carry the read-only bearer
// the client already attaches. It exposes the in-process counters plus the
// per-route request-latency histogram (internal/observe/handler/metrics_live.go).
const observeMetricsLivePath = "/observe/metrics/live"

// LiveMetrics mirrors the subset of go-api's GET /observe/metrics/live the
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
// of observed latencies (ms), the fixed histogram buckets, and the 2xx/4xx/5xx
// status-class tally. It mirrors go-api's reqmetrics.RouteLatency shape;
// percentiles and error rate are derived by the bucket, not here — this stays a
// pure read.
type RouteLatency struct {
	Count   uint64          `json:"count"`
	SumMs   uint64          `json:"sum_ms"`
	Buckets []LatencyBucket `json:"buckets"`
	Status  StatusClasses   `json:"status"`
}

// StatusClasses is one route's 2xx/4xx/5xx response tally, mirroring go-api's
// reqmetrics.StatusClasses. Statuses outside these classes are not counted. An
// older go-api that predates the status counts sends no "status" block; it decodes
// to the zero value, so the derived error rate reads zero rather than failing.
type StatusClasses struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

// LatencyBucket is one histogram bucket: the count of requests at or below LeMs
// milliseconds. Counts are per-bucket (not cumulative). The final bucket carries
// the label "+Inf" and holds everything slower than the last fixed bound, so it
// has no finite upper bound.
type LatencyBucket struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

// AdminMetricsLive fetches GET /observe/metrics/live and decodes its per-route
// latency histogram. It reuses the read primitive, so the read-only bearer, the
// host pin, the bounded body and the timeout all apply: an unreachable go-api
// yields a SourceDownError, a rejected token or a principal the admin gate refuses yields an
// APIError, and a runaway body cannot exhaust memory. It is a read; nothing here
// writes, commands or mutates go-api.
func (c *Client) AdminMetricsLive(ctx context.Context) (LiveMetrics, error) {
	var out LiveMetrics
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return LiveMetrics{}, err
	}
	return out, nil
}

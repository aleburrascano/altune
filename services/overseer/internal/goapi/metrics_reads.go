package goapi

import "context"

const observeMetricsLivePath = "/observe/metrics/live"

type LiveMetrics struct {
	Latency LatencyMetrics `json:"latency"`
}

type LatencyMetrics struct {
	Routes map[string]RouteLatency `json:"routes"`
}

type RouteLatency struct {
	Count   uint64          `json:"count"`
	SumMs   uint64          `json:"sum_ms"`
	Buckets []LatencyBucket `json:"buckets"`
	Status  StatusClasses   `json:"status"`
}

type StatusClasses struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

type LatencyBucket struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

func (c *Client) AdminMetricsLive(ctx context.Context) (LiveMetrics, error) {
	var out LiveMetrics
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return LiveMetrics{}, err
	}
	return out, nil
}

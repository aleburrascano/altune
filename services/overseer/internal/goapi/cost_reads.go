package goapi

import (
	"context"
	"math"
)

// AdminProviderUsage fetches GET /observe/metrics/live and decodes only its
// per-provider outbound-call counts (the "providers" field), the provider-usage
// half of the Cost bucket. It is additive: a second, focused view of the same
// operator endpoint the Back-end performance bucket reads for latency, decoding a
// disjoint field so neither read constrains the other and go-api adding counters
// never fails either decode.
//
// It reuses the read primitive, so the read-only bearer, the host pin, the bounded
// body and the timeout all apply: an unreachable go-api yields a SourceDownError,
// a rejected token or a principal the admin gate refuses yields an APIError, and a runaway
// body cannot exhaust memory. It is a read; nothing here writes, commands or
// mutates go-api. The path const lives in metrics_reads.go; this file never edits
// client.go.
func (c *Client) AdminProviderUsage(ctx context.Context) (ProviderUsage, error) {
	var out providerUsageEnvelope
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

// providerUsageEnvelope decodes just the "providers" field of the live-metrics
// snapshot, ignoring the per-module counters and the latency histogram the other
// reads model. Keeping the envelope private and returning only the map keeps this
// a focused provider-usage mirror.
type providerUsageEnvelope struct {
	Providers ProviderUsage `json:"providers"`
}

// ProviderUsage is the per-provider outbound-call breakdown from go-api's cost
// enabler, keyed by provider label (e.g. "deezer", "spotify", "other"). It
// mirrors go-api's providermetrics.Snapshot shape. The keys are a fixed, bounded
// provider set go-api derives from request hosts — never a URL, query or body —
// so the map carries only provider names and integer counts, no PII. Provider
// names are still HTML-escaped at render time, never trusted as markup.
type ProviderUsage map[string]ProviderOutcomes

// ProviderOutcomes is one provider's call breakdown by outcome: OK responses,
// quota/4xx rejections, and transport/5xx errors. Counts are cumulative int64s
// mirroring go-api's expvar counters.
type ProviderOutcomes struct {
	OK    int64 `json:"ok"`
	Quota int64 `json:"quota"`
	Error int64 `json:"error"`
}

// Total is the provider's total observed calls across all outcomes. The sum is
// saturating, not a plain +: the counters are monotonic expvar integers well
// below the int64 ceiling under any benign go-api, but a hostile or corrupt
// response near the ceiling would wrap a plain int64 add — a huge positive total
// could wrap negative, which desyncs the render gates (counted inactive by
// Total() > 0 yet rendered by Total() == 0, or the reverse). satAddInt64 pins the
// sum at MaxInt64 instead, so the total stays large-and-positive and Total() is
// zero only when every outcome is genuinely zero.
func (o ProviderOutcomes) Total() int64 {
	return satAddInt64(satAddInt64(o.OK, o.Quota), o.Error)
}

// satAddInt64 adds two int64 counters, saturating at the int64 bounds instead of
// wrapping on overflow. Overflow can only happen when both operands share a sign
// and the result flips sign; provider counts are non-negative, so the MaxInt64
// arm is the one that matters, but the MinInt64 arm keeps a hostile negative pair
// from wrapping upward too.
func satAddInt64(a, b int64) int64 {
	sum := a + b
	switch {
	case a > 0 && b > 0 && sum < 0:
		return math.MaxInt64
	case a < 0 && b < 0 && sum >= 0:
		return math.MinInt64
	default:
		return sum
	}
}

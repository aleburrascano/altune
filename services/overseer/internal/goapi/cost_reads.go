package goapi

import "context"

// AdminProviderUsage fetches GET /admin/metrics/live and decodes only its
// per-provider outbound-call counts (the "providers" field), the provider-usage
// half of the Cost bucket. It is additive: a second, focused view of the same
// operator endpoint the Back-end performance bucket reads for latency, decoding a
// disjoint field so neither read constrains the other and go-api adding counters
// never fails either decode.
//
// It reuses the read primitive, so the operator bearer, the host pin, the bounded
// body and the timeout all apply: an unreachable go-api yields a SourceDownError,
// a rejected token or a non-operator principal yields an APIError, and a runaway
// body cannot exhaust memory. It is a read; nothing here writes, commands or
// mutates go-api. The path const lives in metrics_reads.go; this file never edits
// client.go.
func (c *Client) AdminProviderUsage(ctx context.Context) (ProviderUsage, error) {
	var out providerUsageEnvelope
	if err := c.get(ctx, adminMetricsLivePath, &out); err != nil {
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
// taken in int64; the counters are monotonic expvar integers well below the
// int64 ceiling under any benign go-api, and a hostile response near the ceiling
// only affects this provider's own displayed total, never another's.
func (o ProviderOutcomes) Total() int64 { return o.OK + o.Quota + o.Error }

package goapi

import (
	"context"
	"math"
)

func (c *Client) AdminProviderUsage(ctx context.Context) (ProviderUsage, error) {
	var out providerUsageEnvelope
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

type providerUsageEnvelope struct {
	Providers ProviderUsage `json:"providers"`
}

type ProviderUsage map[string]ProviderOutcomes

type ProviderOutcomes struct {
	OK    int64 `json:"ok"`
	Quota int64 `json:"quota"`
	Error int64 `json:"error"`
}

func (o ProviderOutcomes) Total() int64 {
	return satAddInt64(satAddInt64(o.OK, o.Quota), o.Error)
}

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

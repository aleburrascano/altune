package goapihttp

import (
	"altune/overseer/internal/goapi/internal/core"
	"context"
	"net/http"
)

const (
	observeHealthPath             = "/observe/health"
	observeMetricsLivePath        = "/observe/metrics/live"
	observeEvalPath               = "/observe/eval"
	observeAcquisitionPath        = "/observe/acquisition"
	observeDiscographyQualityPath = "/observe/quality/discography"
)

func (c *Client) AdminHealth(ctx context.Context) (core.OperatorHealth, error) {
	var out core.OperatorHealth
	if err := c.get(ctx, observeHealthPath, &out, http.StatusServiceUnavailable); err != nil {
		return core.OperatorHealth{}, err
	}
	return out, nil
}

func (c *Client) AdminMetricsLive(ctx context.Context) (core.LiveMetrics, error) {
	var out core.LiveMetrics
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return core.LiveMetrics{}, err
	}
	return out, nil
}

type providerUsageEnvelope struct {
	Providers core.ProviderUsage `json:"providers"`
}

func (c *Client) AdminProviderUsage(ctx context.Context) (core.ProviderUsage, error) {
	var out providerUsageEnvelope
	if err := c.get(ctx, observeMetricsLivePath, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

func (c *Client) AdminEval(ctx context.Context) (core.EvalStatus, error) {
	var out core.EvalStatus
	if err := c.get(ctx, observeEvalPath, &out); err != nil {
		return core.EvalStatus{}, err
	}
	return out, nil
}

func (c *Client) AdminAcquisition(ctx context.Context) (core.AcquisitionStatus, error) {
	var out core.AcquisitionStatus
	if err := c.get(ctx, observeAcquisitionPath, &out); err != nil {
		return core.AcquisitionStatus{}, err
	}
	return out, nil
}

func (c *Client) AdminDiscographyQuality(ctx context.Context) (core.DiscographyQuality, error) {
	var out core.DiscographyQuality
	if err := c.get(ctx, observeDiscographyQualityPath, &out); err != nil {
		return core.DiscographyQuality{}, err
	}
	return out, nil
}

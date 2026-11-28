package service

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	APIErrorRatioKey         = "api_5xx_ratio"
	apiRoutePrefix           = "/v1/"
	apiErrorWindow           = 5 * time.Minute
	apiErrorMinRequests      = 20
	apiErrorRatioThreshold   = 0.05
	apiErrorMaxSampleHistory = 64
)

type apiSample struct {
	at       time.Time
	requests uint64
	failures uint64
}

type apiErrorCheck struct {
	requests ports.RequestLatencyReader
	clock    clock.Func
	samples  []apiSample
}

func apiErrorRatioCondition(requests ports.RequestLatencyReader, clk clock.Func) domain.Condition {
	c := &apiErrorCheck{requests: requests, clock: clk}
	return domain.Condition{Key: APIErrorRatioKey, Eval: c.eval}
}

func (c *apiErrorCheck) eval(context.Context) *domain.Alert {
	now := c.clock()
	current := apiTotals(c.requests.RequestLatency(), now)
	baseline := c.record(current)
	requests := current.requests - baseline.requests
	if requests < apiErrorMinRequests {
		return nil
	}
	failures := current.failures - baseline.failures
	ratio := float64(failures) / float64(requests)
	if ratio < apiErrorRatioThreshold {
		return nil
	}
	return &domain.Alert{
		Title: "altune API errors",
		Message: fmt.Sprintf("%d of %d API requests failed with a 5xx in the last %d minutes (%.1f%%, threshold %.0f%%)",
			failures, requests, int(now.Sub(baseline.at).Minutes()), ratio*100, apiErrorRatioThreshold*100),
		Severity: domain.SeverityWatch,
	}
}

func (c *apiErrorCheck) record(current apiSample) apiSample {
	c.samples = append(c.samples, current)
	keepFrom := 0
	for i, s := range c.samples {
		if current.at.Sub(s.at) <= apiErrorWindow {
			break
		}
		keepFrom = i
	}
	c.samples = c.samples[keepFrom:]
	if len(c.samples) > apiErrorMaxSampleHistory {
		c.samples = c.samples[len(c.samples)-apiErrorMaxSampleHistory:]
	}
	return c.samples[0]
}

func apiTotals(snap domain.RequestLatency, at time.Time) apiSample {
	total := apiSample{at: at}
	for route, latency := range snap.Routes {
		if !strings.HasPrefix(route, apiRoutePrefix) {
			continue
		}
		total.requests += latency.Count
		total.failures += latency.Status.Count5xx
	}
	return total
}

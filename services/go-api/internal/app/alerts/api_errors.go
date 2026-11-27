package alerts

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
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
	snapshot func() requestmetrics.Snapshot
	clock    clock.Func
	samples  []apiSample
}

func apiErrorRatioCondition(snapshot func() requestmetrics.Snapshot, clk clock.Func) monitoring.Condition {
	c := &apiErrorCheck{snapshot: snapshot, clock: clk}
	return monitoring.Condition{Key: APIErrorRatioKey, Eval: c.eval}
}

func (c *apiErrorCheck) eval(context.Context) *monitoring.Alert {
	now := c.clock()
	current := apiTotals(c.snapshot(), now)
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
	return &monitoring.Alert{
		Title: "altune API errors",
		Message: fmt.Sprintf("%d of %d API requests failed with a 5xx in the last %d minutes (%.1f%%, threshold %.0f%%)",
			failures, requests, int(now.Sub(baseline.at).Minutes()), ratio*100, apiErrorRatioThreshold*100),
		Severity: monitoring.SeverityWatch,
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

func apiTotals(snap requestmetrics.Snapshot, at time.Time) apiSample {
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

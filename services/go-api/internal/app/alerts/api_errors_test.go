package alerts_test

import (
	"altune/go-api/internal/app/alerts"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/platform/requestmetrics/requestmetricstest"
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"strings"
	"testing"
	"time"
)

type traffic struct {
	routes map[string]requestmetricstest.RouteLatency
}

func (tr *traffic) serve(route string, ok, serverErrors uint64) {
	if tr.routes == nil {
		tr.routes = map[string]requestmetricstest.RouteLatency{}
	}
	r := tr.routes[route]
	r.Count += ok + serverErrors
	r.Status.Count2xx += ok
	r.Status.Count5xx += serverErrors
	tr.routes[route] = r
}

func (tr *traffic) snapshot() requestmetrics.Snapshot {
	routes := make(map[string]requestmetricstest.RouteLatency, len(tr.routes))
	for k, v := range tr.routes {
		routes[k] = v
	}
	return requestmetrics.Snapshot{Routes: routes}
}

func apiErrorCondition(t *testing.T, tr *traffic, clk *clocktest.Manual) monitoring.Condition {
	t.Helper()
	s := sources()
	s.Clock = clk.Func()
	s.Requests = tr.snapshot
	return condition(t, s, alerts.APIErrorRatioKey)
}

func TestAPIErrorRatio_FiresWhenEnoughRecentAPIRequestsFailWith5xx(t *testing.T) {
	tr := &traffic{}
	clk := clocktest.NewManual(testNow)
	cond := apiErrorCondition(t, tr, clk)
	tr.serve("/v1/tracks/{id}", 0, 0)
	if cond.Eval(context.Background()) != nil {
		t.Fatal("fired with no traffic")
	}

	clk.Advance(30 * time.Second)
	tr.serve("/v1/tracks/{id}", 80, 2)
	tr.serve("/v1/discovery/search", 10, 8)
	alert := cond.Eval(context.Background())

	if alert == nil || alert.Severity != monitoring.SeverityWatch {
		t.Fatalf("alert = %+v, want a paging (signal) alert", alert)
	}
	if !strings.Contains(alert.Message, "10 of 100") {
		t.Errorf("message = %q, want the failed and total counts", alert.Message)
	}
}

func TestAPIErrorRatio_StaysQuietBelowTheRatioOrTheMinimumTraffic(t *testing.T) {
	tr := &traffic{}
	clk := clocktest.NewManual(testNow)
	cond := apiErrorCondition(t, tr, clk)
	cond.Eval(context.Background())

	clk.Advance(30 * time.Second)
	tr.serve("/v1/tracks/{id}", 95, 2)
	if a := cond.Eval(context.Background()); a != nil {
		t.Fatalf("fired at 2/97: %+v", a)
	}

	clk.Advance(30 * time.Second)
	tr.serve("/v1/tracks/{id}", 0, 3)
	if a := cond.Eval(context.Background()); a == nil {
		t.Fatal("did not fire at 5/100 over the window")
	}

	quiet := &traffic{}
	quietCond := apiErrorCondition(t, quiet, clocktest.NewManual(testNow))
	quietCond.Eval(context.Background())
	quiet.serve("/v1/tracks/{id}", 0, 5)
	if a := quietCond.Eval(context.Background()); a != nil {
		t.Fatalf("fired on 5 requests, all failing, too few to page on: %+v", a)
	}
}

func TestAPIErrorRatio_IgnoresRoutesThatAreNotTheAPI(t *testing.T) {
	tr := &traffic{}
	clk := clocktest.NewManual(testNow)
	cond := apiErrorCondition(t, tr, clk)
	cond.Eval(context.Background())

	clk.Advance(30 * time.Second)
	tr.serve("/health", 0, 100)
	tr.serve("/observe/health", 0, 100)
	tr.serve("unmatched", 0, 100)
	tr.serve("/v1/tracks/{id}", 100, 0)

	if a := cond.Eval(context.Background()); a != nil {
		t.Fatalf("fired on failures outside /v1/: %+v", a)
	}
}

func TestAPIErrorRatio_RecoversWhenTheFailuresAgeOutOfTheWindow(t *testing.T) {
	tr := &traffic{}
	clk := clocktest.NewManual(testNow)
	cond := apiErrorCondition(t, tr, clk)
	cond.Eval(context.Background())
	clk.Advance(30 * time.Second)
	tr.serve("/v1/tracks/{id}", 50, 50)
	if cond.Eval(context.Background()) == nil {
		t.Fatal("did not fire during the burst")
	}

	for range 12 {
		clk.Advance(time.Minute)
		tr.serve("/v1/tracks/{id}", 100, 0)
		cond.Eval(context.Background())
	}

	if a := cond.Eval(context.Background()); a != nil {
		t.Fatalf("still firing %v after the burst left the window: %+v", 12*time.Minute, a)
	}
}

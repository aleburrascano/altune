package handler

import (
	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/discovery/adapters/providermetrics"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/observe/service/evalmeter"
	"altune/go-api/internal/shared/reqmetrics"
	"context"
	"testing"
	"time"
)

func TestContract_MetricsLiveMatchesFixture(t *testing.T) {
	reqmetrics.Observe("/v1/tracks/{trackId}", 5*time.Millisecond, 200)
	deps := Deps{LiveMetrics: func() LiveMetrics {
		return LiveMetrics{"latency": reqmetrics.ReadSnapshot(), "providers": providermetrics.ReadSnapshot()}
	}}
	rec := serveRead(t, deps, "/metrics/live")
	assertMatchesFixture(t, "observe_metrics_live.json", rec.Body.Bytes())
}

func TestContract_EvalMatchesFixture(t *testing.T) {
	runner := func(context.Context) (evalmeter.Result, error) {
		return evalmeter.Result{
			Score:    0.9,
			Baseline: 0.8,
			Queries:  []evalmeter.QueryResult{{Query: "q", Expect: "e", Passed: true, Position: 1}},
		}, nil
	}
	meter := evalmeter.New(true, time.Hour, runner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meter.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for meter.Status().LastRun == nil {
		if time.Now().After(deadline) {
			t.Fatal("eval meter recorded no run within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	rec := serveRead(t, Deps{Eval: meter}, "/eval")
	assertMatchesFixture(t, "observe_eval.json", rec.Body.Bytes())
}

func TestContract_AcquisitionMatchesFixture(t *testing.T) {
	reader := fakeAcquisitionReader{status: acqPorts.AcquisitionStatus{
		InFlight: 1, Succeeded: 2, Failed: 3, Rejected: 4, QueueDepth: 5, QueueCapacity: 6,
	}}
	rec := serveRead(t, Deps{Acquisition: reader}, "/acquisition")
	assertMatchesFixture(t, "observe_acquisition.json", rec.Body.Bytes())
}

func TestContract_QualityMatchesFixture(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []ports.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: ports.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}
	rec := serveRead(t, Deps{Discography: reader}, "/quality/discography")
	assertMatchesFixture(t, "observe_quality_discography.json", rec.Body.Bytes())
}

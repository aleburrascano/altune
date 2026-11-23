package handler_test

import (
	"altune/go-api/internal/observe"
	"context"
	"testing"
	"time"
)

func TestContract_EvalMatchesFixture(t *testing.T) {
	runner := func(context.Context) (observe.EvalMeterResult, error) {
		return observe.EvalMeterResult{
			Score:    0.9,
			Baseline: 0.8,
			Queries:  []observe.EvalMeterQueryResult{{Query: "q", Expect: "e", Passed: true, Position: 1}},
		}, nil
	}
	meter := observe.NewEvalMeter(time.Hour, runner)
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

	rec := serveRead(t, observe.Deps{Eval: meter}, "/eval")
	assertMatchesFixture(t, "observe_eval.json", rec.Body.Bytes())
}

func TestContract_AcquisitionMatchesFixture(t *testing.T) {
	reader := fakeAcquisitionReader{status: observe.AcquisitionStatus{
		InFlight: 1, Succeeded: 2, Failed: 3, Rejected: 4, QueueDepth: 5, QueueCapacity: 6,
	}}
	rec := serveRead(t, observe.Deps{Acquisition: reader}, "/acquisition")
	assertMatchesFixture(t, "observe_acquisition.json", rec.Body.Bytes())
}

func TestContract_QualityMatchesFixture(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []observe.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: observe.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}
	rec := serveRead(t, observe.Deps{Discography: reader}, "/quality/discography")
	assertMatchesFixture(t, "observe_quality_discography.json", rec.Body.Bytes())
}

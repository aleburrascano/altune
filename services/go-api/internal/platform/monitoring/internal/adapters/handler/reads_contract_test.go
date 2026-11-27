package handler_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock"
	"context"
	"testing"
	"time"
)

func TestContract_EvalMatchesFixture(t *testing.T) {
	runner := func(context.Context) (monitoring.EvalMeterResult, error) {
		return monitoring.EvalMeterResult{
			Score:    0.9,
			Baseline: 0.8,
			Queries:  []monitoring.EvalMeterQueryResult{{Query: "q", Expect: "e", Passed: true, Position: 1}},
		}, nil
	}
	meter, err := monitoring.NewEvalMeter(clock.Now, runner, func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if err := meter.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}

	rec := serveRead(t, monitoring.Deps{Eval: meter}, "/eval")
	assertMatchesFixture(t, "observe_eval.json", rec.Body.Bytes())
}

func TestContract_AcquisitionMatchesFixture(t *testing.T) {
	reader := fakeAcquisitionReader{status: monitoring.DownloadStatusReport{
		InFlight: 1, Succeeded: 2, Failed: 3, Rejected: 4, QueueDepth: 5, QueueCapacity: 6,
	}}
	rec := serveRead(t, monitoring.Deps{Acquisition: reader}, "/acquisition")
	assertMatchesFixture(t, "observe_acquisition.json", rec.Body.Bytes())
}

func TestContract_QualityMatchesFixture(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []monitoring.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: monitoring.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}
	rec := serveRead(t, monitoring.Deps{Discography: reader}, "/quality/discography")
	assertMatchesFixture(t, "observe_quality_discography.json", rec.Body.Bytes())
}

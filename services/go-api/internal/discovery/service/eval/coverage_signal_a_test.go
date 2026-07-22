package eval

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
)

// fakeEventQuery returns canned aggregates; no DB.
type fakeEventQuery struct {
	zero      []ports.QueryCount
	noClick   []ports.QueryCount
	abandoned []ports.QueryCount
}

func (f *fakeEventQuery) ZeroResultQueries(_ context.Context, _ time.Time, _ int) ([]ports.QueryCount, error) {
	return f.zero, nil
}

func (f *fakeEventQuery) NonZeroNoClickQueries(_ context.Context, _ time.Time, _ int) ([]ports.QueryCount, error) {
	return f.noClick, nil
}

func (f *fakeEventQuery) AbandonedSearches(_ context.Context, _ time.Time, _ int) ([]ports.QueryCount, error) {
	return f.abandoned, nil
}

// fakeCorrector reports a correction exists for any query_norm in its set.
type fakeCorrector struct {
	typos map[string]bool
}

func (f *fakeCorrector) Correct(_ context.Context, query string) *service.CorrectionResult {
	if f.typos[query] {
		return &service.CorrectionResult{Corrected: query + " (fixed)", Confidence: 0.9}
	}
	return nil
}

func TestCoverageSignalA(t *testing.T) {
	t.Run("zero-result queries surface as strong gaps ranked by count", func(t *testing.T) {
		events := &fakeEventQuery{zero: []ports.QueryCount{
			{QueryNorm: "obscure band", Count: 9},
			{QueryNorm: "rare track", Count: 3},
		}}
		svc := NewCoverageSignalAService(events, nil)

		report, err := svc.Execute(context.Background(), time.Time{}, 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(report.Strong) != 2 {
			t.Fatalf("strong gaps = %d, want 2", len(report.Strong))
		}
		if report.Strong[0].QueryNorm != "obscure band" || report.Strong[0].Count != 9 {
			t.Errorf("top strong gap = %+v, want obscure band/9", report.Strong[0])
		}
		if report.Strong[0].Strength != GapStrong {
			t.Errorf("strength = %v, want strong", report.Strong[0].Strength)
		}
	})

	t.Run("correctable typo is filtered out, not a strong gap", func(t *testing.T) {
		events := &fakeEventQuery{zero: []ports.QueryCount{
			{QueryNorm: "kendrik lamar", Count: 5}, // typo — correction exists
			{QueryNorm: "real gap", Count: 2},
		}}
		corrector := &fakeCorrector{typos: map[string]bool{"kendrik lamar": true}}
		svc := NewCoverageSignalAService(events, corrector)

		report, err := svc.Execute(context.Background(), time.Time{}, 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.FilteredAsTypos != 1 {
			t.Errorf("FilteredAsTypos = %d, want 1", report.FilteredAsTypos)
		}
		if len(report.Strong) != 1 || report.Strong[0].QueryNorm != "real gap" {
			t.Errorf("strong = %+v, want only [real gap]", report.Strong)
		}
	})

	t.Run("no-click queries are weak hints only, never strong", func(t *testing.T) {
		events := &fakeEventQuery{
			zero:    nil,
			noClick: []ports.QueryCount{{QueryNorm: "browsed not clicked", Count: 4}},
		}
		svc := NewCoverageSignalAService(events, nil)

		report, err := svc.Execute(context.Background(), time.Time{}, 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(report.Strong) != 0 {
			t.Errorf("strong = %d, want 0", len(report.Strong))
		}
		if len(report.Weak) != 1 || report.Weak[0].Strength != GapWeak {
			t.Errorf("weak = %+v, want one weak gap", report.Weak)
		}
	})

	t.Run("reformulated no-click queries surface as abandoned gaps", func(t *testing.T) {
		events := &fakeEventQuery{
			abandoned: []ports.QueryCount{{QueryNorm: "gave up and retyped", Count: 6}},
		}
		svc := NewCoverageSignalAService(events, nil)

		report, err := svc.Execute(context.Background(), time.Time{}, 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(report.Abandoned) != 1 || report.Abandoned[0].Strength != GapAbandoned {
			t.Errorf("abandoned = %+v, want one abandoned gap", report.Abandoned)
		}
		if report.Abandoned[0].QueryNorm != "gave up and retyped" || report.Abandoned[0].Count != 6 {
			t.Errorf("abandoned gap = %+v, want gave up and retyped/6", report.Abandoned[0])
		}
	})

	t.Run("empty telemetry yields an empty report, no error", func(t *testing.T) {
		svc := NewCoverageSignalAService(&fakeEventQuery{}, nil)

		report, err := svc.Execute(context.Background(), time.Time{}, 50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(report.Strong) != 0 || len(report.Weak) != 0 || len(report.Abandoned) != 0 || report.FilteredAsTypos != 0 {
			t.Errorf("expected empty report, got %+v", report)
		}
	})
}

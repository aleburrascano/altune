package app

import (
	"context"
	"testing"
	"time"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

// fakeCoverageEvents models the discovery event query. topN is what the capped
// ZeroResultQueries(limit) returns (the top-1000 list); total is the unbounded
// true count of zero-result searches in the window.
type fakeCoverageEvents struct {
	topN  []discoveryPorts.QueryCount
	total int
}

func (f *fakeCoverageEvents) ZeroResultTotal(context.Context, time.Time) (int, error) {
	return f.total, nil
}

func (f *fakeCoverageEvents) ZeroResultQueries(_ context.Context, _ time.Time, limit int) ([]discoveryPorts.QueryCount, error) {
	if len(f.topN) > limit {
		return f.topN[:limit], nil
	}
	return f.topN, nil
}

func TestBuildCoverageCondition(t *testing.T) {
	ctx := context.Background()

	t.Run("breach beyond the top-1000 cap still fires (undercount regression)", func(t *testing.T) {
		// 1500 distinct zero-result queries, one hit each: true total 1500.
		// The top-1000 list sums to only 1000, which would miss a threshold of 1200.
		rows := make([]discoveryPorts.QueryCount, 1500)
		for i := range rows {
			rows[i] = discoveryPorts.QueryCount{QueryNorm: "q", Count: 1}
		}
		events := &fakeCoverageEvents{topN: rows, total: 1500}

		cappedSum := 0
		list, _ := events.ZeroResultQueries(ctx, time.Time{}, 1000)
		for _, r := range list {
			cappedSum += r.Count
		}
		if cappedSum >= 1200 {
			t.Fatalf("capped sum = %d, want < 1200 so the old code would miss the breach", cappedSum)
		}

		cond := buildCoverageCondition(events, 1200)
		alert := cond.Eval(ctx)
		if alert == nil {
			t.Fatalf("alert did not fire: true total %d breaches threshold 1200 but capped sum %d hid it", events.total, cappedSum)
		}
	})

	t.Run("true total below threshold does not fire", func(t *testing.T) {
		events := &fakeCoverageEvents{
			topN:  []discoveryPorts.QueryCount{{QueryNorm: "rare", Count: 3}},
			total: 3,
		}
		if alert := buildCoverageCondition(events, 10).Eval(ctx); alert != nil {
			t.Fatalf("alert fired with total %d below threshold 10: %+v", events.total, alert)
		}
	})

	t.Run("true total at threshold fires", func(t *testing.T) {
		events := &fakeCoverageEvents{
			topN:  []discoveryPorts.QueryCount{{QueryNorm: "missing band", Count: 42}},
			total: 50,
		}
		alert := buildCoverageCondition(events, 50).Eval(ctx)
		if alert == nil {
			t.Fatalf("alert did not fire at total == threshold")
		}
		if alert.Title != "altune discovery coverage gap" {
			t.Errorf("title = %q", alert.Title)
		}
	})
}

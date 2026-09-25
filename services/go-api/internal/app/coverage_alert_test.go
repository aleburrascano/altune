package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	adminAlert "altune/go-api/internal/admin/alert"

	discoveryPorts "altune/go-api/internal/discovery/ports"
)

// fakeCoverageEvents models the discovery event query. topN is what the capped
// ZeroResultQueries(limit) returns (the top-1000 list); total is the unbounded
// true count of zero-result searches in the window. totalErr, when set, makes
// ZeroResultTotal fail.
type fakeCoverageEvents struct {
	topN     []discoveryPorts.QueryCount
	total    int
	totalErr error
}

func (f *fakeCoverageEvents) ZeroResultTotal(context.Context, time.Time) (int, error) {
	if f.totalErr != nil {
		return 0, f.totalErr
	}
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

		cond, _ := buildCoverageConditions(events, 1200)
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
		gap, _ := buildCoverageConditions(events, 10)
		if alert := gap.Eval(ctx); alert != nil {
			t.Fatalf("alert fired with total %d below threshold 10: %+v", events.total, alert)
		}
	})

	t.Run("true total at threshold fires", func(t *testing.T) {
		events := &fakeCoverageEvents{
			topN:  []discoveryPorts.QueryCount{{QueryNorm: "missing band", Count: 42}},
			total: 50,
		}
		gap, _ := buildCoverageConditions(events, 50)
		alert := gap.Eval(ctx)
		if alert == nil {
			t.Fatalf("alert did not fire at total == threshold")
		}
		if alert.Title != "altune discovery coverage gap" {
			t.Errorf("title = %q", alert.Title)
		}
	})
}

// The coverage alert is logged for the operator, so it must carry counts
// only and never the user's search text.
func TestBuildCoverageCondition_MessageExcludesQueryText(t *testing.T) {
	const query = "my private search \"quoted\" term"
	events := &fakeCoverageEvents{
		topN:  []discoveryPorts.QueryCount{{QueryNorm: query, Count: 42}},
		total: 50,
	}
	gap, _ := buildCoverageConditions(events, 10)
	alert := gap.Eval(context.Background())
	if alert == nil {
		t.Fatal("alert did not fire above threshold")
	}
	for _, fragment := range []string{query, "private", "quoted"} {
		if strings.Contains(alert.Message, fragment) || strings.Contains(alert.Title, fragment) {
			t.Fatalf("alert leaks query text %q: title=%q message=%q", fragment, alert.Title, alert.Message)
		}
	}
	for _, want := range []string{"50", "10", "42"} {
		if !strings.Contains(alert.Message, want) {
			t.Errorf("message %q lost count %s", alert.Message, want)
		}
	}
}

// evalTick runs the conditions in registration order, as the monitor does on
// one tick, and returns the alerts that fired keyed by condition key.
func evalTick(ctx context.Context, conds ...adminAlert.Condition) map[string]*adminAlert.Alert {
	fired := make(map[string]*adminAlert.Alert)
	for _, c := range conds {
		if a := c.Eval(ctx); a != nil {
			fired[c.Key] = a
		}
	}
	return fired
}

// A failing coverage query must never read as "checked, no gap": the gap
// verdict holds through the streak and the failure pages on its own key.
func TestBuildCoverageConditions_QueryFailureIsNotHealthy(t *testing.T) {
	ctx := context.Background()

	t.Run("persistent failure escalates to its own signal", func(t *testing.T) {
		events := &fakeCoverageEvents{totalErr: errors.New("relation zero_result does not exist")}
		gap, queryFailing := buildCoverageConditions(events, 10)

		for i := 1; i < coverageQueryFailureEscalation; i++ {
			if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
				t.Fatalf("failure %d fired %v before escalation threshold %d", i, fired, coverageQueryFailureEscalation)
			}
		}
		alert := evalTick(ctx, gap, queryFailing)[queryFailing.Key]
		if alert == nil {
			t.Fatalf("%d consecutive query failures resolved to healthy; want a failure signal", coverageQueryFailureEscalation)
		}
		if queryFailing.Key == gap.Key {
			t.Errorf("failure signal shares key %q with the gap alert", gap.Key)
		}
		if alert.Severity != adminAlert.SeveritySignal {
			t.Errorf("severity = %v, want SeveritySignal", alert.Severity)
		}
		if strings.Contains(alert.Message, "relation") || strings.Contains(alert.Message, "zero_result") {
			t.Errorf("failure message leaks the driver error: %q", alert.Message)
		}
	})

	t.Run("failure while a gap is firing does not resolve the gap", func(t *testing.T) {
		events := &fakeCoverageEvents{total: 50}
		gap, queryFailing := buildCoverageConditions(events, 10)
		if evalTick(ctx, gap, queryFailing)[gap.Key] == nil {
			t.Fatal("gap did not fire above threshold")
		}
		events.totalErr = errors.New("conn reset")
		if evalTick(ctx, gap, queryFailing)[gap.Key] == nil {
			t.Fatal("a query failure resolved a firing gap alert as healthy")
		}
	})

	t.Run("a successful query resets the failure streak", func(t *testing.T) {
		events := &fakeCoverageEvents{totalErr: errors.New("timeout")}
		gap, queryFailing := buildCoverageConditions(events, 10)
		for range coverageQueryFailureEscalation {
			evalTick(ctx, gap, queryFailing)
		}
		events.totalErr = nil
		events.total = 1
		if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
			t.Fatalf("fired %v after the query recovered below threshold", fired)
		}
	})
}

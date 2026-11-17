package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"sync/atomic"
	"testing"
)

type alternatingChecker struct{ calls atomic.Int64 }

func (a *alternatingChecker) Health(context.Context) (goapi.Health, error) {
	if a.calls.Add(1)%2 == 0 {
		return goapi.Health{}, srcDown("GET /health")
	}
	return degraded()
}

func TestSnapshotNeverPairsDownWithDegraded(t *testing.T) {
	const polls = 20000
	checker := &alternatingChecker{}
	b := reliability.New(reliability.Deps{Reader: &fakeReader{}, Checker: checker, Interval: fastInterval})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		b.Wait()
	})
	b.Start(ctx)

	torn := 0
	for checker.calls.Load() < polls {
		snap := b.Snapshot()
		if snap.State == core.StateSourceDown && (snap.Reason == goapi.ReasonDegraded || snapData(t, snap).Reachability == goapi.ReasonDegraded) {
			torn++
		}
	}
	if torn > 0 {
		t.Fatalf("%d snapshots paired a down probe with a degraded reason", torn)
	}
}

func TestReachOutcomeIsOneConsistentPair(t *testing.T) {
	cases := []struct {
		name       string
		probe      []reply
		wantState  core.State
		wantReason string
		wantReach  string
	}{
		{"connecting", nil, core.StateStale, "", goapi.StatusConnecting.String()},
		{"up", []reply{{health: goapi.Health{Status: "ok"}}}, core.StateLive, "", goapi.StatusUp.String()},
		{"degraded", []reply{{health: goapi.Health{Status: "degraded"}}}, core.StateLive, goapi.ReasonDegraded, goapi.ReasonDegraded},
		{"down", []reply{{err: srcDown("GET /health")}}, core.StateSourceDown, "", goapi.StatusDown.String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, probe := probedBucket(t, &fakeReader{})
			for _, r := range tc.probe {
				probe.answer(r.health, r.err)
			}

			snap := b.Snapshot()
			if snap.State != tc.wantState {
				t.Errorf("state = %q, want %q", snap.State, tc.wantState)
			}
			if snap.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", snap.Reason, tc.wantReason)
			}
			if got := snapData(t, snap).Reachability; got != tc.wantReach {
				t.Errorf("reachability = %q, want %q", got, tc.wantReach)
			}
		})
	}
}

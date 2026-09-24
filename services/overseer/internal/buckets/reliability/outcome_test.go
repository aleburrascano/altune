package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

type alternatingChecker struct{ calls atomic.Int64 }

func (a *alternatingChecker) Health(context.Context) (goapi.Health, error) {
	if a.calls.Add(1)%2 == 0 {
		return goapi.Health{}, srcDown("GET /health")
	}
	return goapi.Health{Status: "degraded"}, nil
}

func TestSnapshotNeverPairsDownWithDegraded(t *testing.T) {
	b := newBucket(&fakeReader{}, &alternatingChecker{}, defaultPollInterval)
	const polls = 20000
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Go(func() {
		defer close(done)
		for range polls {
			b.poller.pollOnce(context.Background())
		}
	})
	torn := 0
	for {
		select {
		case <-done:
			wg.Wait()
			if torn > 0 {
				t.Fatalf("%d snapshots paired a down probe with a degraded reason", torn)
			}
			return
		default:
		}
		snap := b.Snapshot()
		if snap.State == core.StateSourceDown && (snap.Reason == goapi.ReasonDegraded || snapData(t, snap).Reachability == goapi.ReasonDegraded) {
			torn++
		}
	}
}

func TestReachOutcomeIsOneConsistentPair(t *testing.T) {
	cases := []struct {
		outcome    reachOutcome
		wantStatus goapi.Status
		wantReason string
		wantText   string
	}{
		{outcomeConnecting, goapi.StatusConnecting, "", goapi.StatusConnecting.String()},
		{outcomeUp, goapi.StatusUp, "", goapi.StatusUp.String()},
		{outcomeDegraded, goapi.StatusUp, goapi.ReasonDegraded, goapi.ReasonDegraded},
		{outcomeDown, goapi.StatusDown, "", goapi.StatusDown.String()},
	}
	for _, tc := range cases {
		t.Run(tc.wantText, func(t *testing.T) {
			if got := tc.outcome.status(); got != tc.wantStatus {
				t.Errorf("status = %v, want %v", got, tc.wantStatus)
			}
			if got := tc.outcome.reason(); got != tc.wantReason {
				t.Errorf("reason = %q, want %q", got, tc.wantReason)
			}
			if got := tc.outcome.String(); got != tc.wantText {
				t.Errorf("String = %q, want %q", got, tc.wantText)
			}
		})
	}
}

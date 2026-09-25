package usage

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"testing"
)

func TestReconnectingSourceReportsConnectingReason(t *testing.T) {
	cases := []struct {
		status     goapi.Status
		wantState  core.State
		wantReason string
	}{
		{goapi.StatusConnecting, core.StateStale, "connecting"},
		{goapi.StatusUp, core.StateLive, ""},
	}
	for _, tc := range cases {
		t.Run(tc.status.String(), func(t *testing.T) {
			src := newFakeSource(1)
			src.setStatus(tc.status)

			snap := newBucket(src).Snapshot()

			if snap.State != tc.wantState || snap.Reason != tc.wantReason {
				t.Errorf("snapshot state %q reason %q, want %q/%q", snap.State, snap.Reason, tc.wantState, tc.wantReason)
			}
		})
	}
}

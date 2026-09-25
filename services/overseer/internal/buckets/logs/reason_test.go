package logs

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
			snap := newBucket(newFakeSource(tc.status, 0), "").Snapshot()

			if snap.State != tc.wantState || snap.Reason != tc.wantReason {
				t.Errorf("snapshot state %q reason %q, want %q/%q", snap.State, snap.Reason, tc.wantState, tc.wantReason)
			}
		})
	}
}

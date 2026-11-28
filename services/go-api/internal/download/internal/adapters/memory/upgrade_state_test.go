package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"
)

func TestUpgradeState_SettleKeepsTheRejectedKeysAndResetsEvidenceOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	state := memory.NewUpgradeState()
	id := ids.NewTrackId()

	if err := state.SettleAcquisition(ctx, id, 0, []string{"a"}, ports.KeepEvidence); err != nil {
		t.Fatal(err)
	}
	if got, _ := state.State(ctx, id); len(got.RejectedSourceKeys) != 1 || got.Evidence != nil {
		t.Fatalf("State after a keep = %+v, want the key and no evidence", got)
	}
	if err := state.SettleAcquisition(ctx, id, 0, []string{"b"}, ports.ResetEvidence); err != nil {
		t.Fatal(err)
	}

	got, _ := state.State(ctx, id)
	if len(got.RejectedSourceKeys) != 2 || string(got.Evidence) != "{}" {
		t.Errorf("State after a reset = %+v, want both keys and empty evidence", got)
	}
}

func TestUpgradeState_AnUnknownTrackHasNoState(t *testing.T) {
	got, err := memory.NewUpgradeState().State(context.Background(), ids.NewTrackId())

	if err != nil || len(got.RejectedSourceKeys) != 0 || got.Evidence != nil {
		t.Errorf("State = %+v, %v, want empty", got, err)
	}
}

package monitoringfromplayback_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromplayback"
	"altune/go-api/internal/shared/telemetry"
	"encoding/json"
	"testing"
)

func TestSnapshot_ReadsThePlaybackCountersFromTheRegistry(t *testing.T) {
	metrics := telemetry.NewRegistry()
	metrics.Counter("playback_corrupt_stored_state_total").Add(2)

	raw, err := json.Marshal(monitoringfromplayback.NewSnapshot(metrics).Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		CorruptStoredState int64 `json:"corrupt_stored_state_total"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if got.CorruptStoredState != 2 {
		t.Errorf("corrupt_stored_state_total = %d, want 2: %s", got.CorruptStoredState, raw)
	}
}

package monitoringfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromlibrary"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/telemetry"
	"encoding/json"
	"testing"
)

func TestSnapshot_ReadsThePresignFailuresFromTheRegistry(t *testing.T) {
	metrics := telemetry.NewRegistry()
	library.NewAudioStoreMetrics(metrics).PresignFailed()

	raw, err := json.Marshal(monitoringfromlibrary.NewSnapshot(metrics).Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		PresignFailures int64 `json:"presign_failures_total"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if got.PresignFailures != 1 {
		t.Errorf("presign_failures_total = %d, want 1: %s", got.PresignFailures, raw)
	}
}

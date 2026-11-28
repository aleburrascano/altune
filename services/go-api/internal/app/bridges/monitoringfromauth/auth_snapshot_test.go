package monitoringfromauth_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromauth"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/telemetry"
	"encoding/json"
	"testing"
)

func TestSnapshot_ReadsTheAuthCountersFromTheRegistry(t *testing.T) {
	metrics := telemetry.NewRegistry()
	auth.NewAuthMetrics(metrics).TokenRejected("signature_invalid")

	raw, err := json.Marshal(monitoringfromauth.NewSnapshot(metrics).Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		TokenRejections int64 `json:"token_rejections_total"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if got.TokenRejections != 1 {
		t.Errorf("token_rejections_total = %d, want 1: %s", got.TokenRejections, raw)
	}
}

package metrics_test

import (
	"altune/go-api/internal/platform/auth/internal/adapters/metrics"
	"altune/go-api/internal/shared/telemetry"
	"testing"
)

func TestAuthMetrics_CountIntoTheInjectedRegistryOnly(t *testing.T) {
	reg := telemetry.NewRegistry()
	other := telemetry.NewRegistry()
	m := metrics.NewAuthMetrics(reg)

	m.TokenRejected("expired")
	m.TokenRejected("expired")
	m.TokenRejected("signature_invalid")
	m.RequestThrottled()
	m.RequestThrottled()
	m.VerifierUnavailable()
	m.JWKSFetchFailed()

	got := metrics.ReadSnapshot(reg)
	if got.TokenRejections != 3 || got.TokenRejectionsByReason["expired"] != 2 || got.TokenRejectionsByReason["signature_invalid"] != 1 {
		t.Errorf("rejections = %d by reason %v, want 3 with expired 2 and signature_invalid 1", got.TokenRejections, got.TokenRejectionsByReason)
	}
	if got.RequestsThrottled != 2 || got.VerifierUnavailable != 1 || got.JWKSFetchFailures != 1 {
		t.Errorf("snapshot = %+v, want 2 throttled, 1 unavailable, 1 jwks failure", got)
	}
	if untouched := metrics.ReadSnapshot(other); untouched.TokenRejections != 0 || untouched.RequestsThrottled != 0 {
		t.Errorf("another registry saw %+v, want zeros: the metrics are not process globals", untouched)
	}
}

func TestAuthMetrics_KeepTheirWireNames(t *testing.T) {
	want := map[string]string{
		metrics.TokenRejectionsVar:         "auth_token_rejections_total",
		metrics.TokenRejectionsByReasonVar: "auth_token_rejections_by_reason_total",
		metrics.RequestsThrottledVar:       "auth_requests_throttled_total",
		metrics.VerifierUnavailableVar:     "auth_verifier_unavailable_total",
		metrics.JWKSFetchFailuresVar:       "auth_jwks_fetch_failures_total",
	}
	for got, name := range want {
		if got != name {
			t.Errorf("metric name = %q, want %q", got, name)
		}
	}
}

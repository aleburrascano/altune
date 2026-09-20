package metrics

import (
	"encoding/json"
	"expvar"
	"testing"
)

func TestExpvarAuthMetrics_PublishesAndIncrements(t *testing.T) {
	m := NewExpvarAuthMetrics()

	before := ReadSnapshot()
	m.TokenRejected("expired")
	m.TokenRejected("expired")
	m.TokenRejected("signature_invalid")
	m.RequestThrottled()
	m.RequestThrottled()
	m.VerifierUnavailable()
	m.JWKSFetchFailed()
	after := ReadSnapshot()

	if after.TokenRejections != before.TokenRejections+3 {
		t.Errorf("TokenRejections = %d, want %d", after.TokenRejections, before.TokenRejections+3)
	}
	for reason, n := range map[string]int64{"expired": 2, "signature_invalid": 1} {
		if got, want := after.TokenRejectionsByReason[reason], before.TokenRejectionsByReason[reason]+n; got != want {
			t.Errorf("TokenRejectionsByReason[%s] = %d, want %d", reason, got, want)
		}
	}
	if after.RequestsThrottled != before.RequestsThrottled+2 {
		t.Errorf("RequestsThrottled = %d, want %d", after.RequestsThrottled, before.RequestsThrottled+2)
	}
	if after.VerifierUnavailable != before.VerifierUnavailable+1 {
		t.Errorf("VerifierUnavailable = %d, want %d", after.VerifierUnavailable, before.VerifierUnavailable+1)
	}
	if after.JWKSFetchFailures != before.JWKSFetchFailures+1 {
		t.Errorf("JWKSFetchFailures = %d, want %d", after.JWKSFetchFailures, before.JWKSFetchFailures+1)
	}

	// The counters are published under their documented expvar names.
	for _, name := range []string{TokenRejectionsVar, TokenRejectionsByReasonVar, RequestsThrottledVar, VerifierUnavailableVar, JWKSFetchFailuresVar} {
		v := expvar.Get(name)
		if v == nil {
			t.Fatalf("expvar %q was never published", name)
		}
		if !json.Valid([]byte(v.String())) {
			t.Errorf("expvar %q = %q, not valid JSON", name, v.String())
		}
	}
}

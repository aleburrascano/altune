// Package metrics provides an expvar-backed implementation of the auth metrics
// port. The counters are process-global published values; operators read them
// through the operator-only GET /admin/metrics/live route.
package metrics

import (
	"altune/go-api/internal/auth/ports"
	"expvar"
)

// Published expvar variable names for auth's degradation counters.
const (
	TokenRejectionsVar         = "auth_token_rejections_total"
	TokenRejectionsByReasonVar = "auth_token_rejections_by_reason_total"
	RequestsThrottledVar       = "auth_requests_throttled_total"
	VerifierUnavailableVar     = "auth_verifier_unavailable_total"
	JWKSFetchFailuresVar       = "auth_jwks_fetch_failures_total"
)

// Declared at package scope because expvar.NewInt/NewMap panic on a duplicate
// name; registering once keeps the adapter safe to construct any number of
// times.
var (
	tokenRejections         = expvar.NewInt(TokenRejectionsVar)
	tokenRejectionsByReason = expvar.NewMap(TokenRejectionsByReasonVar)
	requestsThrottled       = expvar.NewInt(RequestsThrottledVar)
	verifierUnavailable     = expvar.NewInt(VerifierUnavailableVar)
	jwksFetchFailures       = expvar.NewInt(JWKSFetchFailuresVar)
)

// ExpvarAuthMetrics implements ports.AuthMetrics by incrementing
// process-global expvar counters.
type ExpvarAuthMetrics struct{}

var _ ports.AuthMetrics = ExpvarAuthMetrics{}

// NewExpvarAuthMetrics returns an ExpvarAuthMetrics.
func NewExpvarAuthMetrics() ExpvarAuthMetrics { return ExpvarAuthMetrics{} }

// TokenRejected bumps both the overall rejection count (the one number to alert
// on) and the per-reason breakdown (to tell a key-rotation bug's
// signature_invalid spike from client clock skew's expired spike).
func (ExpvarAuthMetrics) TokenRejected(reason string) {
	tokenRejections.Add(1)
	tokenRejectionsByReason.Add(reason, 1)
}

func (ExpvarAuthMetrics) RequestThrottled()    { requestsThrottled.Add(1) }
func (ExpvarAuthMetrics) VerifierUnavailable() { verifierUnavailable.Add(1) }
func (ExpvarAuthMetrics) JWKSFetchFailed()     { jwksFetchFailures.Add(1) }

// Snapshot is a point-in-time read of the auth counters, shaped for JSON
// exposure.
type Snapshot struct {
	TokenRejections         int64            `json:"token_rejections_total"`
	TokenRejectionsByReason map[string]int64 `json:"token_rejections_by_reason_total"`
	RequestsThrottled       int64            `json:"requests_throttled_total"`
	VerifierUnavailable     int64            `json:"verifier_unavailable_total"`
	JWKSFetchFailures       int64            `json:"jwks_fetch_failures_total"`
}

// ReadSnapshot returns the current values of the published auth counters. It is
// a read-only accessor over the package-scope expvar vars so callers can expose
// these specific counters without reaching the raw expvar registry (which also
// publishes process globals like cmdline and memstats).
func ReadSnapshot() Snapshot {
	byReason := map[string]int64{}
	tokenRejectionsByReason.Do(func(kv expvar.KeyValue) {
		if n, ok := kv.Value.(*expvar.Int); ok {
			byReason[kv.Key] = n.Value()
		}
	})
	return Snapshot{
		TokenRejections:         tokenRejections.Value(),
		TokenRejectionsByReason: byReason,
		RequestsThrottled:       requestsThrottled.Value(),
		VerifierUnavailable:     verifierUnavailable.Value(),
		JWKSFetchFailures:       jwksFetchFailures.Value(),
	}
}

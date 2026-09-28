package metrics

import (
	"altune/go-api/internal/auth/ports"
	"expvar"
)

const (
	TokenRejectionsVar         = "auth_token_rejections_total"
	TokenRejectionsByReasonVar = "auth_token_rejections_by_reason_total"
	RequestsThrottledVar       = "auth_requests_throttled_total"
	VerifierUnavailableVar     = "auth_verifier_unavailable_total"
	JWKSFetchFailuresVar       = "auth_jwks_fetch_failures_total"
)

var (
	tokenRejections         = expvar.NewInt(TokenRejectionsVar)
	tokenRejectionsByReason = expvar.NewMap(TokenRejectionsByReasonVar)
	requestsThrottled       = expvar.NewInt(RequestsThrottledVar)
	verifierUnavailable     = expvar.NewInt(VerifierUnavailableVar)
	jwksFetchFailures       = expvar.NewInt(JWKSFetchFailuresVar)
)

type ExpvarAuthMetrics struct{}

var _ ports.AuthMetrics = ExpvarAuthMetrics{}

func NewExpvarAuthMetrics() ExpvarAuthMetrics { return ExpvarAuthMetrics{} }

func (ExpvarAuthMetrics) TokenRejected(reason string) {
	tokenRejections.Add(1)
	tokenRejectionsByReason.Add(reason, 1)
}

func (ExpvarAuthMetrics) RequestThrottled()    { requestsThrottled.Add(1) }
func (ExpvarAuthMetrics) VerifierUnavailable() { verifierUnavailable.Add(1) }
func (ExpvarAuthMetrics) JWKSFetchFailed()     { jwksFetchFailures.Add(1) }

type Snapshot struct {
	TokenRejections         int64            `json:"token_rejections_total"`
	TokenRejectionsByReason map[string]int64 `json:"token_rejections_by_reason_total"`
	RequestsThrottled       int64            `json:"requests_throttled_total"`
	VerifierUnavailable     int64            `json:"verifier_unavailable_total"`
	JWKSFetchFailures       int64            `json:"jwks_fetch_failures_total"`
}

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

package metrics

import (
	"altune/go-api/internal/platform/auth/internal/core/ports"
	"altune/go-api/internal/shared/telemetry"
)

const (
	TokenRejectionsVar         = "auth_token_rejections_total"
	TokenRejectionsByReasonVar = "auth_token_rejections_by_reason_total"
	RequestsThrottledVar       = "auth_requests_throttled_total"
	VerifierUnavailableVar     = "auth_verifier_unavailable_total"
	JWKSFetchFailuresVar       = "auth_jwks_fetch_failures_total"
)

type RegistryAuthMetrics struct {
	tokenRejections         *telemetry.Counter
	tokenRejectionsByReason *telemetry.LabeledCounter
	requestsThrottled       *telemetry.Counter
	verifierUnavailable     *telemetry.Counter
	jwksFetchFailures       *telemetry.Counter
}

var _ ports.AuthMetrics = RegistryAuthMetrics{}

func NewAuthMetrics(reg *telemetry.Registry) RegistryAuthMetrics {
	return RegistryAuthMetrics{
		tokenRejections:         reg.Counter(TokenRejectionsVar),
		tokenRejectionsByReason: reg.LabeledCounter(TokenRejectionsByReasonVar),
		requestsThrottled:       reg.Counter(RequestsThrottledVar),
		verifierUnavailable:     reg.Counter(VerifierUnavailableVar),
		jwksFetchFailures:       reg.Counter(JWKSFetchFailuresVar),
	}
}

func (m RegistryAuthMetrics) TokenRejected(reason string) {
	m.tokenRejections.Add(1)
	m.tokenRejectionsByReason.Add(reason, 1)
}

func (m RegistryAuthMetrics) RequestThrottled()    { m.requestsThrottled.Add(1) }
func (m RegistryAuthMetrics) VerifierUnavailable() { m.verifierUnavailable.Add(1) }
func (m RegistryAuthMetrics) JWKSFetchFailed()     { m.jwksFetchFailures.Add(1) }

type Snapshot struct {
	TokenRejections         int64            `json:"token_rejections_total"`
	TokenRejectionsByReason map[string]int64 `json:"token_rejections_by_reason_total"`
	RequestsThrottled       int64            `json:"requests_throttled_total"`
	VerifierUnavailable     int64            `json:"verifier_unavailable_total"`
	JWKSFetchFailures       int64            `json:"jwks_fetch_failures_total"`
}

func ReadSnapshot(reg *telemetry.Registry) Snapshot {
	return Snapshot{
		TokenRejections:         reg.Counter(TokenRejectionsVar).Value(),
		TokenRejectionsByReason: reg.LabeledCounter(TokenRejectionsByReasonVar).Values(),
		RequestsThrottled:       reg.Counter(RequestsThrottledVar).Value(),
		VerifierUnavailable:     reg.Counter(VerifierUnavailableVar).Value(),
		JWKSFetchFailures:       reg.Counter(JWKSFetchFailuresVar).Value(),
	}
}

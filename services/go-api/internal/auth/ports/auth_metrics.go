package ports

// AuthMetrics aggregates counters that reveal degradation on the authentication
// surface, so a token-rejection spike or a JWKS outage is one dashboarded
// number instead of scattered auth.token_rejected / auth.verifier_unavailable
// log lines.
type AuthMetrics interface {
	// TokenRejected records one request refused with 401. reason is the
	// auth.TokenRejectReason value (a closed set of constants, so it is safe to
	// key a counter by).
	TokenRejected(reason string)
	// RequestThrottled records one request refused with 429 by the failure
	// throttle. It never reached the verifier, so without this counter a
	// brute-force source disappears from the rejection counters exactly when it
	// starts being contained.
	RequestThrottled()
	// VerifierUnavailable records one request refused with 503 because the
	// verifier could not run (e.g. no JWKS key set could be obtained).
	VerifierUnavailable()
	// JWKSFetchFailed records one failed JWKS fetch: the startup fetch, a
	// request-forced refresh, or a background refresh.
	JWKSFetchFailed()
}

// NoopAuthMetrics returns an AuthMetrics that records nothing. It is the
// default so auth stays usable without a metrics backend wired in.
func NoopAuthMetrics() AuthMetrics { return noopAuthMetrics{} }

type noopAuthMetrics struct{}

func (noopAuthMetrics) TokenRejected(string) {}
func (noopAuthMetrics) RequestThrottled()    {}
func (noopAuthMetrics) VerifierUnavailable() {}
func (noopAuthMetrics) JWKSFetchFailed()     {}

package ports

type AuthMetrics interface {
	TokenRejected(reason string)
	RequestThrottled()
	VerifierUnavailable()
	JWKSFetchFailed()
}

func NoopAuthMetrics() AuthMetrics { return noopAuthMetrics{} }

type noopAuthMetrics struct{}

func (noopAuthMetrics) TokenRejected(string) {}
func (noopAuthMetrics) RequestThrottled()    {}
func (noopAuthMetrics) VerifierUnavailable() {}
func (noopAuthMetrics) JWKSFetchFailed()     {}

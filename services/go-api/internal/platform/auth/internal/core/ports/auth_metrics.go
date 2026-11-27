package ports

type AuthMetrics interface {
	TokenRejected(reason string)
	RequestThrottled()
	VerifierUnavailable()
	JWKSFetchFailed()
}

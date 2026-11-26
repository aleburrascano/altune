package auth

import ports "altune/go-api/internal/platform/auth/internal/core/ports"

type (
	InvalidTokenError = ports.InvalidTokenError
	TokenVerifier     = ports.TokenVerifier
	VerifiedToken     = ports.VerifiedToken
	VerifierFunc      = ports.VerifierFunc
)

const ReasonSignatureInvalid = ports.ReasonSignatureInvalid

var (
	NoopAuthMetrics  = ports.NoopAuthMetrics
	NoopTokenRevoker = ports.NoopTokenRevoker
)

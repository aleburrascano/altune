package auth

import ports "altune/go-api/internal/auth/internal/core/ports"

type (
	TokenVerifier = ports.TokenVerifier
	VerifierFunc  = ports.VerifierFunc
)

var NoopTokenRevoker = ports.NoopTokenRevoker

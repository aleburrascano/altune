package auth

import ports "altune/go-api/internal/platform/auth/internal/core/ports"

type (
	InvalidTokenError = ports.InvalidTokenError
	TokenVerifier     = ports.TokenVerifier
)

const ReasonSignatureInvalid = ports.ReasonSignatureInvalid

var NoopTokenRevoker = ports.NoopTokenRevoker

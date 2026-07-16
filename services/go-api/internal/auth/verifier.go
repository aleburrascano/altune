package auth

import (
	"context"

	"altune/go-api/internal/shared"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (shared.UserId, error)
}

// VerifierFunc adapts a plain function to TokenVerifier, mirroring
// http.HandlerFunc. Tests pass a closure instead of each package defining its
// own stub type.
type VerifierFunc func(ctx context.Context, token string) (shared.UserId, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (shared.UserId, error) {
	return f(ctx, token)
}

type TokenRejectReason string

const (
	ReasonMissing          TokenRejectReason = "missing"
	ReasonMalformed        TokenRejectReason = "malformed"
	ReasonSignatureInvalid TokenRejectReason = "signature_invalid"
	ReasonExpired          TokenRejectReason = "expired"
	ReasonClaimInvalidISS  TokenRejectReason = "claim_invalid_iss"
	ReasonClaimInvalidAUD  TokenRejectReason = "claim_invalid_aud"
	ReasonClaimInvalidSUB  TokenRejectReason = "claim_invalid_sub"
)

type InvalidTokenError struct {
	Reason TokenRejectReason
	Detail string
}

func (e *InvalidTokenError) Error() string {
	if e.Detail != "" {
		return "invalid token: " + e.Detail
	}
	return "invalid token: " + string(e.Reason)
}

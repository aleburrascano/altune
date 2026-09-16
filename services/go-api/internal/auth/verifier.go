package auth

import (
	"altune/go-api/internal/shared"
	"context"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (shared.UserId, error)
}

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
	ReasonClaimMissingEXP  TokenRejectReason = "claim_missing_exp"
	ReasonClaimInvalidISS  TokenRejectReason = "claim_invalid_iss"
	ReasonClaimInvalidAUD  TokenRejectReason = "claim_invalid_aud"
	ReasonClaimInvalidSUB  TokenRejectReason = "claim_invalid_sub"
	// ReasonClaimInvalidIAT covers a missing iat claim or an exp-iat lifetime
	// longer than the verifier's accepted maximum (see supabase_jwt.go).
	ReasonClaimInvalidIAT TokenRejectReason = "claim_invalid_iat"
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

package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (VerifiedToken, error)
}

type VerifierFunc func(ctx context.Context, token string) (VerifiedToken, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (VerifiedToken, error) {
	return f(ctx, token)
}

type VerifiedToken struct {
	UserID    shared.UserId
	ExpiresAt time.Time
}

func (t VerifiedToken) contextFor(ctx context.Context) context.Context {
	return ContextWithTokenExpiry(ContextWithUserID(ctx, t.UserID), t.ExpiresAt)
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
	ReasonClaimInvalidIAT  TokenRejectReason = "claim_invalid_iat"
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

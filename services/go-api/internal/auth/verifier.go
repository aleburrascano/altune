package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

// TokenVerifier turns a bearer token into the caller's identity.
//
// The error kind is the contract, and Middleware branches on it: an
// *InvalidTokenError means the caller's token is at fault, so the request gets
// 401 and is counted as a token rejection; any other error means the verifier
// could not run (no key set, a timeout, a cancelled context), so the request
// gets 503 and is counted as verifier unavailability. An implementation that
// returns a bare error for a bad token therefore reports an outage of itself.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (shared.UserId, error)
}

type VerifierFunc func(ctx context.Context, token string) (shared.UserId, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (shared.UserId, error) {
	return f(ctx, token)
}

type VerifiedToken struct {
	UserID    shared.UserId
	ExpiresAt time.Time
}

type ExpiringTokenVerifier interface {
	VerifyExpiring(ctx context.Context, token string) (VerifiedToken, error)
}

func VerifyToken(ctx context.Context, v TokenVerifier, token string) (VerifiedToken, error) {
	if expiring, ok := v.(ExpiringTokenVerifier); ok {
		return expiring.VerifyExpiring(ctx, token)
	}
	userID, err := v.Verify(ctx, token)
	return VerifiedToken{UserID: userID}, err
}

func (t VerifiedToken) contextFor(ctx context.Context) context.Context {
	ctx = ContextWithUserID(ctx, t.UserID)
	if t.ExpiresAt.IsZero() {
		return ctx
	}
	return ContextWithTokenExpiry(ctx, t.ExpiresAt)
}

// TokenRejectReason names why a bearer token was refused. The constants below
// are the whole set: each value is used verbatim as a metrics label
// (ports.AuthMetrics.TokenRejected) and as the 401 body's "reason", so a new
// reason is added here rather than formatted at a call site, which is what
// keeps the counter off an unbounded, caller-influenced label.
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

// InvalidTokenError is the rejection a caller caused, the one error kind
// Middleware answers with 401. Reason is what the client and the counter see;
// Detail only ever reaches the logs, through Error.
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

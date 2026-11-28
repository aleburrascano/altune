package ports

import (
	"altune/go-api/internal/platform/auth/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"time"
)

type AuthMetrics interface {
	TokenRejected(reason string)
	RequestThrottled()
	VerifierUnavailable()
	JWKSFetchFailed()
}

type TokenRevoker interface {
	Revoked(ctx context.Context, userID ids.UserId, issuedAt time.Time) (bool, error)
}

func NoopTokenRevoker() TokenRevoker { return noopTokenRevoker{} }

type noopTokenRevoker struct{}

func (noopTokenRevoker) Revoked(context.Context, ids.UserId, time.Time) (bool, error) {
	return false, nil
}

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (domain.VerifiedToken, error)
}

type VerifierFunc func(ctx context.Context, token string) (domain.VerifiedToken, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (domain.VerifiedToken, error) {
	return f(ctx, token)
}

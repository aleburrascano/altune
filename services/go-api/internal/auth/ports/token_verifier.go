package ports

import (
	"altune/go-api/internal/auth/domain"
	"context"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (domain.VerifiedToken, error)
}

type VerifierFunc func(ctx context.Context, token string) (domain.VerifiedToken, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (domain.VerifiedToken, error) {
	return f(ctx, token)
}

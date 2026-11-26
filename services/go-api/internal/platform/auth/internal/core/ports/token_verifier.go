package ports

import (
	"context"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (VerifiedToken, error)
}

type VerifierFunc func(ctx context.Context, token string) (VerifiedToken, error)

func (f VerifierFunc) Verify(ctx context.Context, token string) (VerifiedToken, error) {
	return f(ctx, token)
}

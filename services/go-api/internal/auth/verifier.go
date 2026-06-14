package auth

import (
	"context"

	"altune/go-api/internal/shared"
)

type TokenVerifier interface {
	Verify(ctx context.Context, token string) (shared.UserId, error)
}

type InvalidTokenError struct {
	Reason string
}

func (e *InvalidTokenError) Error() string {
	return "invalid token: " + e.Reason
}

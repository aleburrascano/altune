package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type TokenRevoker interface {
	Revoked(ctx context.Context, userID shared.UserId, issuedAt time.Time) (bool, error)
}

func NoopTokenRevoker() TokenRevoker { return noopTokenRevoker{} }

type noopTokenRevoker struct{}

func (noopTokenRevoker) Revoked(context.Context, shared.UserId, time.Time) (bool, error) {
	return false, nil
}

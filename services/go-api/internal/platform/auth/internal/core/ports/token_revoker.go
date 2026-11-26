package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"time"
)

type TokenRevoker interface {
	Revoked(ctx context.Context, userID ids.UserId, issuedAt time.Time) (bool, error)
}

func NoopTokenRevoker() TokenRevoker { return noopTokenRevoker{} }

type noopTokenRevoker struct{}

func (noopTokenRevoker) Revoked(context.Context, ids.UserId, time.Time) (bool, error) {
	return false, nil
}

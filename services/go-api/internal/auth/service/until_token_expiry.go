package service

import (
	"altune/go-api/internal/auth/domain"
	"context"
	"time"
)

type tokenExpiryKey struct{}

func ContextWithTokenExpiry(ctx context.Context, expiresAt time.Time) context.Context {
	return context.WithValue(ctx, tokenExpiryKey{}, expiresAt)
}

func TokenExpiryFromContext(ctx context.Context) (time.Time, bool) {
	expiresAt, ok := ctx.Value(tokenExpiryKey{}).(time.Time)
	return expiresAt, ok
}

func UntilTokenExpiry(ctx context.Context) (context.Context, context.CancelFunc) {
	expiresAt, ok := TokenExpiryFromContext(ctx)
	if !ok {
		return context.WithCancel(ctx)
	}
	return context.WithDeadlineCause(ctx, expiresAt, domain.ErrTokenExpired)
}

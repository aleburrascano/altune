package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"time"
)

type contextKey struct{}

type tokenExpiryKey struct{}

var userIDKey contextKey

var ErrTokenExpired = errors.New("auth: token expired")

func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

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
	return context.WithDeadlineCause(ctx, expiresAt, ErrTokenExpired)
}

func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	id, ok := ctx.Value(userIDKey).(shared.UserId)
	return id, ok
}

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		rejectToken(w, r, ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}

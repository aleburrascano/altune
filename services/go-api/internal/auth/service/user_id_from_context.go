package service

import (
	"altune/go-api/internal/shared"
	"context"
)

type userIDKey struct{}

func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	id, ok := ctx.Value(userIDKey{}).(shared.UserId)
	return id, ok
}

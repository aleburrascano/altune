package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

type contextKey struct{}

var userIDKey contextKey

func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey, id)
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

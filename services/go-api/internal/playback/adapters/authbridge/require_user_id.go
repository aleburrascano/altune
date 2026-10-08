package authbridge

import (
	"altune/go-api/internal/auth"
	authservice "altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	return auth.RequireUserID(w, r)
}

func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	return authservice.UserIDFromContext(ctx)
}

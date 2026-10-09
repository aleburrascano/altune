package authbridge

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	return middleware.RequireUserID(w, r)
}

var ErrTokenExpired = domain.ErrTokenExpired

func UntilTokenExpiry(ctx context.Context) (context.Context, context.CancelFunc) {
	return service.UntilTokenExpiry(ctx)
}

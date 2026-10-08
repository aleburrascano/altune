package authbridge

import (
	"altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/service"
	"context"
)

var ErrTokenExpired = domain.ErrTokenExpired

func UntilTokenExpiry(ctx context.Context) (context.Context, context.CancelFunc) {
	return service.UntilTokenExpiry(ctx)
}

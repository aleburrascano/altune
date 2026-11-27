package httpuser

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser/internal/core/signedinuser"
	"net/http"
)

func LimitPerUser(limit httpserver.RateLimit, now clock.Func, refused error, onRefused func()) func(http.Handler) http.Handler {
	return httpserver.MustNewUserRateLimiter(limit, now).Middleware(httpserver.UserKey(signedinuser.UserIDFromContext), refused, onRefused)
}

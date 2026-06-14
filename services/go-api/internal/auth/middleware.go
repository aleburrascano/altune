package auth

import (
	"context"
	"net/http"
	"strings"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
)

type contextKey string

const userIDKey contextKey = "userId"

func Middleware(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				httputil.Unauthorized(w, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				httputil.Unauthorized(w, "malformed authorization header")
				return
			}
			token := parts[1]

			userId, err := verifier.Verify(r.Context(), token)
			if err != nil {
				httputil.Unauthorized(w, err.Error())
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	id, ok := ctx.Value(userIDKey).(shared.UserId)
	return id, ok
}

func MustUserID(ctx context.Context) shared.UserId {
	id, ok := UserIDFromContext(ctx)
	if !ok {
		panic("auth middleware not applied: no user ID in context")
	}
	return id
}

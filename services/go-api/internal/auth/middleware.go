package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"altune/go-api/internal/shared"
)

type contextKey string

const userIDKey contextKey = "userId"

func Middleware(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				rejectToken(w, ReasonMissing, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				rejectToken(w, ReasonMalformed, "malformed authorization header")
				return
			}
			token := parts[1]

			userId, err := verifier.Verify(r.Context(), token)
			if err != nil {
				var reason TokenRejectReason
				if ite, ok := err.(*InvalidTokenError); ok {
					reason = ite.Reason
				} else {
					reason = ReasonSignatureInvalid
				}
				rejectToken(w, reason, err.Error())
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userId)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func rejectToken(w http.ResponseWriter, reason TokenRejectReason, detail string) {
	slog.Warn("auth.token_rejected", "reason", string(reason), "detail", detail)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"detail": detail,
		"reason": string(reason),
	})
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

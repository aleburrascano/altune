package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
)

// contextKey is an unexported zero-size type so the user-id key cannot collide
// with a context key from any other package (a bare string could).
type contextKey struct{}

var userIDKey contextKey

func Middleware(verifier TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				rejectToken(w, r, ReasonMissing, "missing authorization header", nil)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				rejectToken(w, r, ReasonMalformed, "malformed authorization header", nil)
				return
			}
			token := parts[1]

			userId, err := verifier.Verify(r.Context(), token)
			if err != nil {
				// errors.As walks the chain, so a wrapped InvalidTokenError still
				// surfaces its specific reason. Anything else means the verifier
				// could not run at all (JWKS unreachable) — that is a 503, not a
				// 401, or an IdP outage reads as "everyone got logged out".
				var ite *InvalidTokenError
				if !errors.As(err, &ite) {
					slog.ErrorContext(r.Context(), "auth.verifier_unavailable",
						"error", err.Error(),
						"path", r.URL.Path,
					)
					httputil.WriteError(w, http.StatusServiceUnavailable, "authentication unavailable")
					return
				}
				rejectToken(w, r, ite.Reason, "invalid token", err)
				return
			}

			slog.DebugContext(r.Context(), "auth.verified",
				"user_id", userId.String(),
				"path", r.URL.Path,
			)

			next.ServeHTTP(w, r.WithContext(ContextWithUserID(r.Context(), userId)))
		})
	}
}

// ContextWithUserID stores a verified user id in the context under the same
// unexported key the middleware uses. Exposed so other packages (and tests) can
// compose an authenticated context without depending on the key's identity.
func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// rejectResponse is the single 401 body shape for the feature: machine-readable
// reason plus a client-safe detail.
type rejectResponse struct {
	Detail string `json:"detail"`
	Reason string `json:"reason"`
}

// rejectToken writes the feature's one 401 contract ({detail, reason} plus a
// WWW-Authenticate challenge). err, when non-nil, is logged server-side only —
// jwx messages and JWKS fetch details never reach an unauthenticated client.
func rejectToken(w http.ResponseWriter, r *http.Request, reason TokenRejectReason, detail string, err error) {
	attrs := []any{"reason", string(reason), "detail", detail}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.WarnContext(r.Context(), "auth.token_rejected", attrs...)

	w.Header().Set("WWW-Authenticate", "Bearer")
	httputil.WriteJSON(w, http.StatusUnauthorized, rejectResponse{
		Detail: detail,
		Reason: string(reason),
	})
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

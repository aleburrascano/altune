package shell

import (
	"altune/overseer/internal/authn"
	"context"
	"log/slog"
	"net/http"
	"strings"
)

type Verifier interface {
	Verify(ctx context.Context, token string) (authn.Claims, error)
}

func OwnerOnly(v Verifier, ownerUserID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				reject(w, r, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := v.Verify(r.Context(), token)
			if err != nil {
				reject(w, r, http.StatusUnauthorized, "invalid token")
				return
			}
			if ownerUserID == "" || claims.Subject != ownerUserID {
				slog.WarnContext(r.Context(), "overseer.auth.non_owner",
					"path", r.URL.Path, "subject", claims.Subject, "remote", r.RemoteAddr)
				reject(w, r, http.StatusForbidden, "not the owner")
				return
			}
			logAccess(r, claims.Subject)
			next.ServeHTTP(w, r)
		})
	}
}

func logAccess(r *http.Request, subject string) {
	slog.InfoContext(r.Context(), "overseer.auth.access",
		"path", r.URL.Path, "subject", subject, "remote", r.RemoteAddr)
}

func reject(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status == http.StatusUnauthorized {
		slog.WarnContext(r.Context(), "overseer.auth.rejected",
			"path", r.URL.Path, "remote", r.RemoteAddr)
	}
	http.Error(w, msg, status)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	scheme, token, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

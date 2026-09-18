package shell

import (
	"altune/overseer/internal/authn"
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// Verifier verifies a Supabase JWT and returns its claims. The shell depends on
// this seam, not the concrete authn.Verifier, so a test can inject a controllable
// verifier and drive the 401/403 paths deterministically.
type Verifier interface {
	Verify(ctx context.Context, token string) (authn.Claims, error)
}

// OwnerOnly enforces the owner-only invariant: a request reaches Overseer data
// only when it carries a bearer JWT that verifies AND whose subject equals the
// single allowlisted owner id. It is the whole access control — one id, no RBAC.
//
//   - No/invalid/expired/forged token, or an unverifiable one -> 401.
//   - A valid token whose subject is not the owner -> 403.
//   - It reads the token from the Authorization: Bearer header ONLY: never a
//     cookie, never a query parameter (which would leak into logs and referrers).
//     No route it guards ever emits Set-Cookie — auth is bearer-only.
//
// It fails closed: an empty configured owner id rejects everything (every subject
// mismatches "").
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
				// A valid non-owner token: authenticated but not authorized. 403, and
				// the subject is logged (it is not a secret) so a wrong-account attempt
				// is diagnosable.
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

// logAccess records one owner read of a guarded route, so "who queried the
// sensitive telemetry" is answerable from the logs — denial records alone leave
// every successful read of /api/stream and the log tail invisible. It is emitted
// before the handler runs: a long-lived SSE connection is audited at connect,
// not hours later when it drops.
//
// It logs the URL path, never the raw URI, so a token smuggled in a query string
// (a request the guard rejects, but the same path a future caller could take)
// cannot land in an audit record. The token itself never reaches the logger.
func logAccess(r *http.Request, subject string) {
	slog.InfoContext(r.Context(), "overseer.auth.access",
		"path", r.URL.Path, "subject", subject, "remote", r.RemoteAddr)
}

// reject writes a bare status with a short, token-free message and logs the
// unauthenticated case. It never sets a cookie.
func reject(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status == http.StatusUnauthorized {
		slog.WarnContext(r.Context(), "overseer.auth.rejected",
			"path", r.URL.Path, "remote", r.RemoteAddr)
	}
	http.Error(w, msg, status)
}

// bearerToken extracts the token from the Authorization: Bearer header. It never
// falls back to a cookie or query parameter. Returns "" when absent or malformed.
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

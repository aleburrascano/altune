package shell

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// cookieName carries the owner token for browser navigation, where an
// Authorization header cannot be set on a top-level page load.
const cookieName = "overseer_token"

// OwnerOnly rejects every request that does not carry the owner token, enforcing
// the "owner-only" invariant: no unauthenticated or non-owner request reaches
// Overseer data. The token is accepted from an Authorization: Bearer header or
// the overseer_token cookie, and compared in constant time.
//
// It fails closed: an empty configured token rejects everything.
func OwnerOnly(ownerToken string) func(http.Handler) http.Handler {
	want := []byte(ownerToken)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(want) == 0 || !tokenMatches(r, want) {
				slog.WarnContext(r.Context(), "overseer.auth.rejected",
					"path", r.URL.Path, "remote", r.RemoteAddr)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func tokenMatches(r *http.Request, want []byte) bool {
	presented := presentedToken(r)
	if presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), want) == 1
}

// presentedToken extracts the owner token from the request, preferring the
// Authorization header over the cookie. It never falls back to query
// parameters, which would leak the token into logs and referrers.
func presentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, found := strings.Cut(h, " ")
		if found && strings.EqualFold(scheme, "bearer") {
			return strings.TrimSpace(token)
		}
		return ""
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

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
//
// basePath is the server-configured mount prefix (never caller-supplied) prefixed
// onto the login redirect so a browser lands back inside the mount when a reverse
// proxy strips the prefix. It is "" by default, yielding the historical "/login".
func OwnerOnly(ownerToken, basePath string) func(http.Handler) http.Handler {
	want := []byte(ownerToken)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(want) != 0 && tokenMatches(r, want) {
				next.ServeHTTP(w, r)
				return
			}
			slog.WarnContext(r.Context(), "overseer.auth.rejected",
				"path", r.URL.Path, "remote", r.RemoteAddr)
			// A browser navigating to the shell with no valid token is sent to the
			// login form, where it can set the cookie. An API/curl client (a bearer
			// header, or anything not asking for HTML) still gets the bare 401, so
			// the programmatic contract is unchanged.
			if wantsLoginRedirect(r) {
				http.Redirect(w, r, basePath+"/login", http.StatusSeeOther)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// wantsLoginRedirect reports whether an unauthenticated request is a top-level
// browser navigation that should land on the login form rather than a 401. It is
// a GET that asks for HTML and carries no Authorization header: presence of that
// header marks an API client, which must keep receiving the 401 path.
func wantsLoginRedirect(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.Header.Get("Authorization") != "" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func tokenMatches(r *http.Request, want []byte) bool {
	presented := presentedToken(r)
	if presented == "" {
		return false
	}
	return constantTimeMatch(presented, want)
}

// constantTimeMatch compares a presented token against the configured owner token
// in constant time, the single comparison the browser-login POST and the header/
// cookie guard both go through. subtle.ConstantTimeCompare also returns 0 on a
// length mismatch, so a prefix of the token never matches.
func constantTimeMatch(presented string, want []byte) bool {
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

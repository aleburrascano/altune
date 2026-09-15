package shell

import (
	"log/slog"
	"net/http"
)

// maxLoginBody caps the /login POST body. The form carries one short token, so a
// larger body is either a mistake or an attempt to exhaust memory; it is refused
// and the request fails closed.
const maxLoginBody = 4096

// loginView is the data the login template renders. Error is a generic,
// token-free message so a rejected attempt never echoes the submitted value.
type loginView struct {
	Error string
}

// handleLoginForm serves the owner-token form. It is reachable without auth — it
// is how the browser owner acquires the cookie in the first place — so it exposes
// no Overseer data, only the form.
func (h *Handler) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	renderLogin(w, r, http.StatusOK, "")
}

// handleLoginSubmit validates the submitted token against the configured owner
// token in constant time. On a match it sets the owner cookie (HttpOnly, Secure,
// SameSite=Strict, Path=/) and 303-redirects to the shell. On a mismatch it
// re-renders the form with a generic error and sets no cookie — it fails closed,
// exactly like OwnerOnly, including the empty-configured-token case.
func (h *Handler) handleLoginSubmit(ownerToken string) http.HandlerFunc {
	want := []byte(ownerToken)
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
		submitted := r.PostFormValue("token")
		if len(want) == 0 || !constantTimeMatch(submitted, want) {
			// Log the rejection without the submitted token: no secret reaches a sink.
			slog.WarnContext(r.Context(), "overseer.login.rejected", "remote", r.RemoteAddr)
			renderLogin(w, r, http.StatusUnauthorized, "Invalid token.")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    ownerToken,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		// Redirect target is the fixed shell root, never caller-supplied, so there
		// is no open-redirect surface on the post-login hop.
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// renderLogin writes the login page with the given status. Security headers are
// set on this token-entry page too, so the form cannot be framed or sniffed.
func renderLogin(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setSecurityHeaders(w)
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, loginView{Error: errMsg}); err != nil {
		slog.ErrorContext(r.Context(), "overseer.login.render", "error", err)
	}
}

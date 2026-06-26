package handler

import (
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
)

// OperatorOnly restricts a route to the single configured operator account.
//
// It MUST be composed after auth.Middleware so a verified user id is present in
// the request context. It re-checks for that id itself (returning 401 when
// absent) so a wiring mistake that skips auth still fails closed rather than
// reaching the comparison with an empty context.
//
// Fail-closed: an empty operatorUserID denies every request — the console is
// off unless OPERATOR_USER_ID is explicitly configured.
func OperatorOnly(operatorUserID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.RequireUserID(w, r)
			if !ok {
				return // RequireUserID already wrote 401
			}
			if operatorUserID == "" || userID.String() != operatorUserID {
				httputil.Forbidden(w, "operator access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

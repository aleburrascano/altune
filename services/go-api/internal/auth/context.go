package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

type contextKey struct{}

var userIDKey contextKey

func ContextWithUserID(ctx context.Context, id shared.UserId) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserIDFromContext returns the identity Middleware stored on the request
// context. It reports false on any route Middleware does not wrap, so absence
// means the request was never authenticated, never that the user is unknown.
func UserIDFromContext(ctx context.Context) (shared.UserId, bool) {
	id, ok := ctx.Value(userIDKey).(shared.UserId)
	return id, ok
}

// RequireUserID returns the identity for a handler that cannot serve without
// one, writing 401 when there is none.
//
// It assumes Middleware ran upstream: Middleware has already refused every bad
// token, so a missing identity here means the route was mounted unwrapped, a
// wiring bug rather than a client's rejected token. The 401 it writes is
// therefore deliberately not counted as a token rejection.
func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		rejectToken(w, r, ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}

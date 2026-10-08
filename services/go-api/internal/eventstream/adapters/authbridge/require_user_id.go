package authbridge

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"net/http"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	return auth.RequireUserID(w, r)
}

package middleware

import (
	"altune/go-api/internal/auth/domain"
	"altune/go-api/internal/auth/service"
	"altune/go-api/internal/shared"
	"net/http"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := service.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, domain.ReasonMissing, "authentication required", nil)
		return shared.UserId{}, false
	}
	return id, true
}

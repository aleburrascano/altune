package httpuser

import (
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/requestuser/internal/core/identity"
	"log/slog"
	"net/http"
)

const (
	detailAuthRequired = "authentication required"
	reasonMissing      = "missing"
)

type unauthorizedResponse struct {
	Detail string `json:"detail"`
	Reason string `json:"reason"`
}

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := identity.UserIDFromContext(r.Context())
	if !ok {
		slog.WarnContext(r.Context(), "auth.token_rejected",
			"reason", reasonMissing,
			"detail", detailAuthRequired,
			"client", ClientKey(r),
			"path", r.URL.Path,
		)
		w.Header().Set("WWW-Authenticate", "Bearer")
		httputil.WriteJSON(w, http.StatusUnauthorized, unauthorizedResponse{
			Detail: detailAuthRequired,
			Reason: reasonMissing,
		})
		return shared.UserId{}, false
	}
	return id, true
}

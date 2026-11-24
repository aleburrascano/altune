package httpuser

import (
	"altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser/internal/core/identity"
	"log/slog"
	"net/http"
)

const (
	detailAuthRequired = "authentication required"
	reasonMissing      = "missing"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (shared.UserId, bool) {
	id, ok := identity.UserIDFromContext(r.Context())
	if !ok {
		slog.WarnContext(r.Context(), "auth.token_rejected",
			"reason", reasonMissing,
			"detail", detailAuthRequired,
			"client", ClientKey(r),
			logging.KeyPath, r.URL.Path,
		)
		w.Header().Set("WWW-Authenticate", "Bearer")
		httputil.HandleServiceError(w, r, domain.NewError(domain.CodeAuthTokenMissing, detailAuthRequired))
		return shared.UserId{}, false
	}
	return id, true
}

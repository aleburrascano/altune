package httpuser

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser/internal/core/signedinuser"
	"log/slog"
	"net/http"
)

const (
	detailAuthRequired = "authentication required"
	reasonMissing      = "missing"
)

func RequireUserID(w http.ResponseWriter, r *http.Request) (ids.UserId, bool) {
	id, ok := signedinuser.UserIDFromContext(r.Context())
	if !ok {
		slog.WarnContext(r.Context(), "auth.token_rejected",
			"reason", reasonMissing,
			"detail", detailAuthRequired,
			"client", ClientKey(r),
			logging.KeyPath, r.URL.Path,
		)
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeAuthTokenMissing, detailAuthRequired))
		return ids.UserId{}, false
	}
	return id, true
}

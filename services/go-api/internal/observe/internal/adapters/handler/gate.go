package handler

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"log/slog"
	"net/http"
)

func Gate(principalID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := requestuser.RequireUserID(w, r)
			if !ok {
				return
			}
			subject := userID.String()
			if !isPrincipal(subject, principalID) {
				logDenial(r, subject, errPrincipalRequired)
				httputil.HandleServiceError(w, r, errPrincipalRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isPrincipal(subject, principalID string) bool {
	return principalID != "" && subject == principalID
}

func logDenial(r *http.Request, actor string, denial *shareddomain.CodedError) {
	slog.WarnContext(r.Context(), "observe.access_denied",
		slog.String("actor", actor),
		slog.String(logging.KeyMethod, r.Method),
		slog.String(logging.KeyPath, r.URL.Path),
		slog.String(logging.KeyCode, denial.Code().String()),
	)
}

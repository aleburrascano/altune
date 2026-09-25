package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
)

func Gate(principalID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.RequireUserID(w, r)
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

func logDenial(r *http.Request, actor string, denial *codedError) {
	slog.WarnContext(r.Context(), "observe.access_denied",
		slog.String("actor", actor),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("code", denial.ErrorCode()),
	)
}

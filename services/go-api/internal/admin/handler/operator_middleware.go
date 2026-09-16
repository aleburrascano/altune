package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

func OperatorOnly(operatorUserID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.RequireUserID(w, r)
			if !ok {
				return
			}
			if operatorUserID == "" || userID.String() != operatorUserID {
				httputil.HandleServiceError(w, r, errOperatorRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

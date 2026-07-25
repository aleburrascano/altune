package handler

import (
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
)

func OperatorOnly(operatorUserID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.RequireUserID(w, r)
			if !ok {
				return
			}
			if operatorUserID == "" || userID.String() != operatorUserID {
				httputil.Forbidden(w, "operator access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

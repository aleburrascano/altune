package testauth

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
	UserID      string `json:"user_id"`
}

func (t *TestAuth) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		token, exp, err := t.IssueToken()
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "could not issue test token")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, loginResponse{
			AccessToken: token,
			TokenType:   "bearer",
			ExpiresAt:   exp.Unix(),
			UserID:      TestUserId().String(),
		})
	}
}

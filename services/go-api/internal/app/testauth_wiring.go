package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/adapters/testauth"
	"altune/go-api/internal/shared/httputil"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func buildTestAuthVerifier(cfg testAuthConfig, supa auth.TokenVerifier) (*testauth.TestAuth, auth.TokenVerifier, error) {
	if !cfg.TestAuthEnabled() {
		return nil, supa, nil
	}
	ta, err := testauth.New()
	if err != nil {
		return nil, nil, err
	}
	return ta, testauth.Combine(ta, supa), nil
}

type testAuthConfig interface {
	TestAuthEnabled() bool
}

func mountTestLogin(r chi.Router, ta *testauth.TestAuth) {
	r.Post("/test/login", testLoginHandler(ta))
}

type testLoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
	UserID      string `json:"user_id"`
}

func testLoginHandler(ta *testauth.TestAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		token, exp, err := ta.IssueToken()
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "could not issue test token")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, testLoginResponse{
			AccessToken: token,
			TokenType:   "bearer",
			ExpiresAt:   exp.Unix(),
			UserID:      testauth.TestUserId().String(),
		})
	}
}

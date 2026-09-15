package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/adapters/testauth"
	"altune/go-api/internal/shared/httputil"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// buildTestAuthVerifier is the single structural guard for the non-production
// test-auth path. When cfg.TestAuthEnabled() is false — a production build, or
// any unrecognised/empty ENV, which the allowlist treats as production — it
// constructs no test verifier and returns (nil, supa): the middleware then uses
// the real Supabase verifier alone, so a test-signed token has nothing that can
// accept it. Only when the guard is true does it build a TestAuth and return a
// combined verifier. The returned *testauth.TestAuth is non-nil exactly when
// the caller must also mount POST /test/login (see mountTestLogin), tying the
// route's presence to the verifier's in one branch.
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

// testAuthConfig is the slice of config the guard reads, kept minimal so the
// decision has one input and is trivially testable.
type testAuthConfig interface {
	TestAuthEnabled() bool
}

// mountTestLogin registers POST /test/login. It is only ever called with a
// non-nil TestAuth, which buildTestAuthVerifier returns only in non-prod, so the
// route cannot exist in production.
func mountTestLogin(r chi.Router, ta *testauth.TestAuth) {
	r.Post("/test/login", testLoginHandler(ta))
}

type testLoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
	UserID      string `json:"user_id"`
}

// testLoginHandler issues a fresh test token for the dedicated test user. It
// takes no credentials: the endpoint only exists in non-prod, and the token it
// returns authenticates as the test user and no one else.
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

package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/adapters/testauth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubConfig struct{ enabled bool }

func (c stubConfig) TestAuthEnabled() bool { return c.enabled }

var (
	realUser      = shared.NewUserId(uuid.New())
	realTokenStr  = "real-supabase-token"
	errNotSupabse = errors.New("not a supabase token")
)

func stubSupabaseVerifier() auth.TokenVerifier {
	return auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == realTokenStr {
			return auth.VerifiedToken{UserID: realUser, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid, Detail: errNotSupabse.Error()}
	})
}

func buildRouter(t *testing.T, cfg testAuthConfig) chi.Router {
	t.Helper()
	testAuth, verifier, err := buildTestAuthVerifier(cfg, stubSupabaseVerifier())
	if err != nil {
		t.Fatalf("buildTestAuthVerifier: %v", err)
	}
	r := chi.NewRouter()
	if testAuth != nil {
		mountTestLogin(r, testAuth)
	}
	r.Group(func(gr chi.Router) {
		gr.Use(authMiddleware(verifier))
		gr.Get("/whoami", func(w http.ResponseWriter, req *http.Request) {
			uid, _ := auth.UserIDFromContext(req.Context())
			_, _ = w.Write([]byte(uid.String()))
		})
	})
	return r
}

func do(t *testing.T, r chi.Router, method, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, r chi.Router) string {
	t.Helper()
	rec := do(t, r, http.MethodPost, "/test/login", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/test/login: got %d, want 200", rec.Code)
	}
	var resp testLoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("login returned an empty access_token")
	}
	return resp.AccessToken
}

func TestWiring_ProdHasNoTestLoginAndRejectsTestTokens(t *testing.T) {
	prod := buildRouter(t, stubConfig{enabled: false})

	if rec := do(t, prod, http.MethodPost, "/test/login", ""); rec.Code != http.StatusNotFound {
		t.Errorf("/test/login in prod: got %d, want 404", rec.Code)
	}

	ta, err := testauth.New()
	if err != nil {
		t.Fatalf("testauth.New: %v", err)
	}
	token, _, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if rec := do(t, prod, http.MethodGet, "/whoami", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("test token in prod: got %d, want 401", rec.Code)
	}

	if rec := do(t, prod, http.MethodGet, "/whoami", realTokenStr); rec.Code != http.StatusOK {
		t.Errorf("real token in prod: got %d, want 200", rec.Code)
	}
}

func assertBackdoorClosed(t *testing.T, cfg testAuthConfig) {
	t.Helper()
	r := buildRouter(t, cfg)

	if rec := do(t, r, http.MethodPost, "/test/login", ""); rec.Code != http.StatusNotFound {
		t.Errorf("/test/login: got %d, want 404", rec.Code)
	}

	ta, err := testauth.New()
	if err != nil {
		t.Fatalf("testauth.New: %v", err)
	}
	token, _, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if rec := do(t, r, http.MethodGet, "/whoami", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("test token: got %d, want 401", rec.Code)
	}
	if rec := do(t, r, http.MethodGet, "/whoami", realTokenStr); rec.Code != http.StatusOK {
		t.Errorf("real token: got %d, want 200", rec.Code)
	}
}

func TestWiring_RealConfigFailsClosedWithoutExplicitOptIn(t *testing.T) {
	t.Run("ENV unset + no opt-in is closed", func(t *testing.T) {
		assertBackdoorClosed(t, &config.Config{})
	})
	t.Run("non-prod ENV but no opt-in is closed", func(t *testing.T) {
		assertBackdoorClosed(t, &config.Config{Env: "development"})
	})
	t.Run("opt-in but production ENV is closed", func(t *testing.T) {
		assertBackdoorClosed(t, &config.Config{Env: "production", TestAuthOptIn: true})
	})
	t.Run("opt-in but unknown ENV is closed", func(t *testing.T) {
		assertBackdoorClosed(t, &config.Config{Env: "staging", TestAuthOptIn: true})
	})
	t.Run("opt-in + non-prod ENV issues an accepted test-user token", func(t *testing.T) {
		dev := buildRouter(t, &config.Config{Env: "development", TestAuthOptIn: true})
		token := login(t, dev)
		rec := do(t, dev, http.MethodGet, "/whoami", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("/whoami with test token: got %d, want 200", rec.Code)
		}
		if got := rec.Body.String(); got != testauth.TestUserId().String() {
			t.Errorf("authenticated as %s, want the test user %s", got, testauth.TestUserId())
		}
	})
}

func TestWiring_NonProdLoginIssuesAcceptedTestUserToken(t *testing.T) {
	dev := buildRouter(t, stubConfig{enabled: true})

	token := login(t, dev)

	rec := do(t, dev, http.MethodGet, "/whoami", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("/whoami with test token: got %d, want 200", rec.Code)
	}
	got := rec.Body.String()
	if got != testauth.TestUserId().String() {
		t.Errorf("authenticated as %s, want the test user %s", got, testauth.TestUserId())
	}
	if got == realUser.String() {
		t.Error("test login authenticated as a real user id")
	}

	if rec := do(t, dev, http.MethodGet, "/whoami", realTokenStr); rec.Body.String() != realUser.String() {
		t.Errorf("real token in non-prod: got %q, want %s", rec.Body.String(), realUser)
	}

	if rec := do(t, dev, http.MethodGet, "/whoami", "garbage"); rec.Code != http.StatusUnauthorized {
		t.Errorf("garbage token in non-prod: got %d, want 401", rec.Code)
	}
}

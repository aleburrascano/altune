package app

import (
	"altune/go-api/internal/auth/adapters/testauth"
	authdomain "altune/go-api/internal/auth/domain"
	authports "altune/go-api/internal/auth/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authMetrics "altune/go-api/internal/auth/adapters/metrics"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"

	authservice "altune/go-api/internal/auth/service"
)

const operatorToken = "operator-token"

func TestMountObserve_AuthRejectionsAndOutagesReachLiveMetrics(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	verifier := authports.VerifierFunc(func(_ context.Context, token string) (authdomain.VerifiedToken, error) {
		switch token {
		case operatorToken:
			return authdomain.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		case "jwks-down":
			return authdomain.VerifiedToken{}, errors.New("fetch JWKS: connection refused")
		}
		return authdomain.VerifiedToken{}, &authdomain.InvalidTokenError{Reason: authdomain.ReasonSignatureInvalid}
	})
	r := chi.NewRouter()
	mountObserveLiveMetrics(r, verifier, operator, liveMetricsSnapshot)

	before := authMetrics.ReadSnapshot()
	if code, _ := callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", "forged"); code != http.StatusUnauthorized {
		t.Fatalf("forged token: status %d, want 401", code)
	}
	if code, _ := callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", "jwks-down"); code != http.StatusServiceUnavailable {
		t.Fatalf("verifier down: status %d, want 503", code)
	}

	code, body := callObserveAs(t, r, http.MethodGet, "/observe/metrics/live", operatorToken)
	if code != http.StatusOK {
		t.Fatalf("operator metrics read: status %d, want 200; body %s", code, body)
	}
	var got struct {
		Auth authMetrics.Snapshot `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Auth.TokenRejections != before.TokenRejections+1 {
		t.Errorf("auth.token_rejections_total %d, want %d", got.Auth.TokenRejections, before.TokenRejections+1)
	}
	reason := string(authdomain.ReasonSignatureInvalid)
	if want := before.TokenRejectionsByReason[reason] + 1; got.Auth.TokenRejectionsByReason[reason] != want {
		t.Errorf("auth.token_rejections_by_reason_total[%s] %d, want %d", reason, got.Auth.TokenRejectionsByReason[reason], want)
	}
	if got.Auth.VerifierUnavailable != before.VerifierUnavailable+1 {
		t.Errorf("auth.verifier_unavailable_total %d, want %d", got.Auth.VerifierUnavailable, before.VerifierUnavailable+1)
	}
}

func TestNewAuthVerifier_NoRevokerConfiguredAcceptsValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub, err := jwk.FromRaw(key.PublicKey)
	if err != nil {
		t.Fatalf("jwk from public key: %v", err)
	}
	_ = pub.Set(jwk.KeyIDKey, "k1")
	_ = pub.Set(jwk.AlgorithmKey, jwa.RS256)
	set := jwk.NewSet()
	_ = set.AddKey(pub)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{SupabaseJWTJWKSURL: srv.URL, SupabaseProjectURL: "https://p.supabase.co", SupabaseJWTAud: "authenticated"}
	verifier, err := newAuthVerifier(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newAuthVerifier: %v", err)
	}

	userID := uuid.New()
	tok := jwt.New()
	_ = tok.Set("sub", userID.String())
	_ = tok.Set("iss", "https://p.supabase.co/auth/v1")
	_ = tok.Set("aud", "authenticated")
	_ = tok.Set("iat", time.Now().Add(-time.Minute))
	_ = tok.Set("exp", time.Now().Add(30*time.Minute))
	priv, err := jwk.FromRaw(key)
	if err != nil {
		t.Fatalf("jwk from private key: %v", err)
	}
	_ = priv.Set(jwk.KeyIDKey, "k1")
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, priv))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	verified, err := verifier.Verify(context.Background(), string(signed))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID != shared.NewUserId(userID) {
		t.Errorf("user id %v, want %v", verified.UserID, userID)
	}
}

type stubConfig struct{ enabled bool }

func (c stubConfig) TestAuthEnabled() bool { return c.enabled }

var (
	realUser      = shared.NewUserId(uuid.New())
	realTokenStr  = "real-supabase-token"
	errNotSupabse = errors.New("not a supabase token")
)

func stubSupabaseVerifier() authports.TokenVerifier {
	return authports.VerifierFunc(func(_ context.Context, token string) (authdomain.VerifiedToken, error) {
		if token == realTokenStr {
			return authdomain.VerifiedToken{UserID: realUser, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return authdomain.VerifiedToken{}, &authdomain.InvalidTokenError{Reason: authdomain.ReasonSignatureInvalid, Detail: errNotSupabse.Error()}
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
		mountTestLogin(t.Context(), r, testAuth)
	}
	r.Group(func(gr chi.Router) {
		gr.Use(authMiddleware(verifier))
		gr.Get("/whoami", func(w http.ResponseWriter, req *http.Request) {
			uid, _ := authservice.UserIDFromContext(req.Context())
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
	var resp struct {
		AccessToken string `json:"access_token"`
	}
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

func TestMountTestLogin_WarnsWhenBackdoorIsMounted(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	buildRouter(t, &config.Config{Env: "development", TestAuthOptIn: true})

	if out := buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "/test/login") {
		t.Fatalf("log = %q, want a WARN naming /test/login", out)
	}
}

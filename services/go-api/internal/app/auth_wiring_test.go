package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/config"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const operatorToken = "operator-token"

func TestMountObserve_AuthRejectionsAndOutagesReachLiveMetrics(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	verifier := auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		switch token {
		case operatorToken:
			return auth.VerifiedToken{UserID: operator, ExpiresAt: time.Now().Add(time.Hour)}, nil
		case "jwks-down":
			return auth.VerifiedToken{}, errors.New("fetch JWKS: connection refused")
		}
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	})
	r := chi.NewRouter()
	mountObserveLiveMetrics(r, verifier, operator, liveMetricsSnapshot)

	before := auth.ReadSnapshot()
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
		Auth auth.MetricsSnapshot `json:"auth"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if got.Auth.TokenRejections != before.TokenRejections+1 {
		t.Errorf("auth.token_rejections_total %d, want %d", got.Auth.TokenRejections, before.TokenRejections+1)
	}
	reason := string(auth.ReasonSignatureInvalid)
	if want := before.TokenRejectionsByReason[reason] + 1; got.Auth.TokenRejectionsByReason[reason] != want {
		t.Errorf("auth.token_rejections_by_reason_total[%s] %d, want %d", reason, got.Auth.TokenRejectionsByReason[reason], want)
	}
	if got.Auth.VerifierUnavailable != before.VerifierUnavailable+1 {
		t.Errorf("auth.verifier_unavailable_total %d, want %d", got.Auth.VerifierUnavailable, before.VerifierUnavailable+1)
	}
}

type jwksFixture struct {
	key *rsa.PrivateKey
	cfg *config.Config
}

func newJWKSFixture(t *testing.T) jwksFixture {
	t.Helper()
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
	return jwksFixture{key: key, cfg: cfg}
}

func (f jwksFixture) signedToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	tok := jwt.New()
	_ = tok.Set("sub", userID.String())
	_ = tok.Set("iss", "https://p.supabase.co/auth/v1")
	_ = tok.Set("aud", "authenticated")
	_ = tok.Set("iat", time.Now().Add(-time.Minute))
	_ = tok.Set("exp", time.Now().Add(30*time.Minute))
	priv, err := jwk.FromRaw(f.key)
	if err != nil {
		t.Fatalf("jwk from private key: %v", err)
	}
	_ = priv.Set(jwk.KeyIDKey, "k1")
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, priv))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return string(signed)
}

func assertVerifiesAs(t *testing.T, verifier auth.TokenVerifier, token string, userID uuid.UUID) {
	t.Helper()
	verified, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID != shared.NewUserId(userID) {
		t.Errorf("user id %v, want %v", verified.UserID, userID)
	}
}

func TestNewAuthVerifier_NoRevokerConfiguredAcceptsValidToken(t *testing.T) {
	f := newJWKSFixture(t)
	verifier, err := newAuthVerifier(context.Background(), f.cfg)
	if err != nil {
		t.Fatalf("newAuthVerifier: %v", err)
	}

	userID := uuid.New()
	assertVerifiesAs(t, verifier, f.signedToken(t, userID), userID)
}

func TestConnectAuth_RefusesAPlaintextJWKSURL(t *testing.T) {
	a := &App{cfg: &config.Config{SupabaseJWTJWKSURL: "http://jwks.example.com/keys", SupabaseProjectURL: "https://p.supabase.co", SupabaseJWTAud: "authenticated"}}

	verifier, err := a.connectAuth(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "auth: ") {
		t.Fatalf("connectAuth error = %v, want the auth-wrapped refusal of a plaintext JWKS URL", err)
	}
	if verifier != nil || a.authVerifier != nil {
		t.Fatalf("connectAuth kept verifier %v / app verifier %v after a failure, want neither", verifier, a.authVerifier)
	}
}

func TestConnectAuth_ServesAndKeepsTheSupabaseVerifier(t *testing.T) {
	f := newJWKSFixture(t)
	a := &App{cfg: f.cfg}

	verifier, err := a.connectAuth(context.Background())
	if err != nil {
		t.Fatalf("connectAuth: %v", err)
	}
	if verifier == nil {
		t.Fatal("connectAuth returned a nil verifier")
	}
	userID := uuid.New()
	assertVerifiesAs(t, verifier, f.signedToken(t, userID), userID)
	if any(a.authVerifier) != any(verifier) {
		t.Fatalf("app keeps %v for health checks, want the verifier connectAuth served", a.authVerifier)
	}
}

package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authMetrics "altune/go-api/internal/auth/adapters/metrics"

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
	reason := string(auth.ReasonSignatureInvalid)
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
	pub, _ := jwk.FromRaw(key.PublicKey)
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
	priv, _ := jwk.FromRaw(key)
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

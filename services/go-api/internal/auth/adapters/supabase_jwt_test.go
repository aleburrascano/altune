package adapters

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"altune/go-api/internal/auth"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// testJWTFixture holds a test RSA key pair, JWKS server, and helper for signing tokens.
type testJWTFixture struct {
	privateKey *rsa.PrivateKey
	jwksServer *httptest.Server
	projectURL string
	audience   string
	issuer     string
	keyID      string
}

func newTestJWTFixture(t *testing.T) *testJWTFixture {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	keyID := "test-key-1"
	pubJWK, err := jwk.FromRaw(privKey.PublicKey)
	if err != nil {
		t.Fatalf("create JWK from public key: %v", err)
	}
	_ = pubJWK.Set(jwk.KeyIDKey, keyID)
	_ = pubJWK.Set(jwk.AlgorithmKey, jwa.RS256)
	_ = pubJWK.Set(jwk.KeyUsageKey, "sig")

	keySet := jwk.NewSet()
	_ = keySet.AddKey(pubJWK)

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	}))
	t.Cleanup(jwksServer.Close)

	projectURL := "https://test-project.supabase.co"
	audience := "authenticated"

	return &testJWTFixture{
		privateKey: privKey,
		jwksServer: jwksServer,
		projectURL: projectURL,
		audience:   audience,
		issuer:     projectURL + "/auth/v1",
		keyID:      keyID,
	}
}

func (f *testJWTFixture) signToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()

	builder := jwt.New()
	for k, v := range claims {
		if err := builder.Set(k, v); err != nil {
			t.Fatalf("set claim %q: %v", k, err)
		}
	}

	privJWK, err := jwk.FromRaw(f.privateKey)
	if err != nil {
		t.Fatalf("create private JWK: %v", err)
	}
	_ = privJWK.Set(jwk.KeyIDKey, f.keyID)
	_ = privJWK.Set(jwk.AlgorithmKey, jwa.RS256)

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, privJWK))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

func (f *testJWTFixture) newVerifier(t *testing.T) *SupabaseJWTVerifier {
	t.Helper()
	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, f.jwksServer.URL, f.projectURL, f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	return verifier
}

func TestSupabaseJWTVerifier_ValidToken(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	sub := uuid.New().String()
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(1 * time.Hour),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	userID, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if userID.String() != sub {
		t.Errorf("userId: got %q, want %q", userID.String(), sub)
	}
}

func TestSupabaseJWTVerifier_ExpiredToken(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(-1 * time.Hour),
		"iat": time.Now().Add(-2 * time.Hour),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != auth.ReasonExpired {
		t.Errorf("reason: got %q, want %q (classifyJWTError may not match jwx v2 error format)", tokenErr.Reason, auth.ReasonExpired)
	}
}

func TestSupabaseJWTVerifier_InvalidSignature(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	// Sign with a different key
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	wrongJWK, err := jwk.FromRaw(wrongKey)
	if err != nil {
		t.Fatalf("create wrong JWK: %v", err)
	}
	_ = wrongJWK.Set(jwk.KeyIDKey, f.keyID)
	_ = wrongJWK.Set(jwk.AlgorithmKey, jwa.RS256)

	builder := jwt.New()
	_ = builder.Set("sub", uuid.New().String())
	_ = builder.Set("iss", f.issuer)
	_ = builder.Set("aud", f.audience)
	_ = builder.Set("exp", time.Now().Add(1*time.Hour))

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, wrongJWK))
	if err != nil {
		t.Fatalf("sign with wrong key: %v", err)
	}

	_, err = verifier.Verify(context.Background(), string(signed))
	if err == nil {
		t.Fatal("expected error for wrong-key signature, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != auth.ReasonSignatureInvalid {
		t.Errorf("reason: got %q, want %q", tokenErr.Reason, auth.ReasonSignatureInvalid)
	}
}

// TestSupabaseJWTVerifier_UnknownKeyID pins classifyJWTError's
// "failed to find key" substring against the real library: a token whose kid
// is absent from the JWKS must classify as signature_invalid, not malformed.
// (TestSupabaseJWTVerifier_InvalidSignature pins "could not verify message".)
func TestSupabaseJWTVerifier_UnknownKeyID(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	strayKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate stray key: %v", err)
	}
	strayJWK, err := jwk.FromRaw(strayKey)
	if err != nil {
		t.Fatalf("create stray JWK: %v", err)
	}
	_ = strayJWK.Set(jwk.KeyIDKey, "not-in-jwks")
	_ = strayJWK.Set(jwk.AlgorithmKey, jwa.RS256)

	builder := jwt.New()
	_ = builder.Set("sub", uuid.New().String())
	_ = builder.Set("iss", f.issuer)
	_ = builder.Set("aud", f.audience)
	_ = builder.Set("exp", time.Now().Add(1*time.Hour))

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, strayJWK))
	if err != nil {
		t.Fatalf("sign with stray key: %v", err)
	}

	_, err = verifier.Verify(context.Background(), string(signed))
	if err == nil {
		t.Fatal("expected error for unknown key id, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != auth.ReasonSignatureInvalid {
		t.Errorf("reason: got %q, want %q (jwx may have reworded 'failed to find key')", tokenErr.Reason, auth.ReasonSignatureInvalid)
	}
}

// TestSupabaseJWTVerifier_JWKSUnavailable asserts that a JWKS fetch failure is
// an infrastructure error, not an InvalidTokenError — the middleware maps it
// to a 503 instead of rejecting the token as invalid.
func TestSupabaseJWTVerifier_JWKSUnavailable(t *testing.T) {
	ctx := context.Background()
	// Port 1 refuses connections; the initial refresh warns, Verify's fetch fails.
	verifier, err := NewSupabaseJWTVerifier(ctx, "http://127.0.0.1:1/jwks", "https://test-project.supabase.co", "authenticated")
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	_, err = verifier.Verify(ctx, "any-token")
	if err == nil {
		t.Fatal("expected error when JWKS is unreachable, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if errors.As(err, &tokenErr) {
		t.Fatalf("JWKS unavailability must not be an InvalidTokenError, got reason %q", tokenErr.Reason)
	}
}

func TestSupabaseJWTVerifier_MissingSub(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	// Token with no sub claim
	token := f.signToken(t, map[string]interface{}{
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(1 * time.Hour),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for missing sub claim, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != auth.ReasonClaimInvalidSUB {
		t.Errorf("reason: got %q, want %q", tokenErr.Reason, auth.ReasonClaimInvalidSUB)
	}
}

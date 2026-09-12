package adapters

import (
	"altune/go-api/internal/auth"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

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
	pubJWK := signingJWK(t, privKey.PublicKey, keyID)

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

	privJWK := signingJWK(t, f.privateKey, f.keyID)

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

// signingJWK builds a JWK from a raw RSA key (public or private), tagging it
// with the key id, RS256 algorithm, and "sig" use so JWKS-published and
// signing keys share one consistent setup.
func signingJWK(t *testing.T, raw any, kid string) jwk.Key {
	t.Helper()
	key, err := jwk.FromRaw(raw)
	if err != nil {
		t.Fatalf("create JWK: %v", err)
	}
	_ = key.Set(jwk.KeyIDKey, kid)
	_ = key.Set(jwk.AlgorithmKey, jwa.RS256)
	_ = key.Set(jwk.KeyUsageKey, "sig")
	return key
}

// assertInvalidTokenReason asserts err unwraps to an *auth.InvalidTokenError
// carrying the wanted reject reason.
func assertInvalidTokenReason(t *testing.T, err error, want auth.TokenRejectReason) {
	t.Helper()
	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != want {
		t.Errorf("reason: got %q, want %q", tokenErr.Reason, want)
	}
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

func TestSupabaseJWTVerifier_ProjectURLTrailingSlash(t *testing.T) {
	f := newTestJWTFixture(t)

	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, f.jwksServer.URL, f.projectURL+"/", f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	sub := uuid.New().String()
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(1 * time.Hour),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	userID, err := verifier.Verify(ctx, token)
	if err != nil {
		t.Fatalf("Verify with trailing-slash project URL: %v", err)
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

	assertInvalidTokenReason(t, err, auth.ReasonExpired)
}

func TestSupabaseJWTVerifier_InvalidSignature(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	wrongJWK := signingJWK(t, wrongKey, f.keyID)

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

	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
}

func TestSupabaseJWTVerifier_UnknownKeyID(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	strayKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate stray key: %v", err)
	}
	strayJWK := signingJWK(t, strayKey, "not-in-jwks")

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

	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
}

func TestSupabaseJWTVerifier_JWKSUnavailable(t *testing.T) {
	ctx := context.Background()
	const unreachableJWKSURL = "http://127.0.0.1:1/jwks"
	verifier, err := NewSupabaseJWTVerifier(ctx, unreachableJWKSURL, "https://test-project.supabase.co", "authenticated")
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

func TestSupabaseJWTVerifier_MissingExp(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"iat": time.Now().Add(-1 * time.Minute),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for token missing exp claim, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonClaimMissingEXP)
}

func TestSupabaseJWTVerifier_SlowJWKSEndpointDoesNotHang(t *testing.T) {
	// A JWKS endpoint that blocks until the test ends, simulating a slow or
	// hung Supabase endpoint. Without a bounded HTTP client the shared fetch
	// worker would block here forever and exhaust the pool for the process.
	blockForever := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-blockForever
	}))
	// Cleanup is LIFO: unblock the handler first so server.Close can return.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(blockForever) })

	// Shorten the bound so the regression test stays fast; restore afterwards.
	origTimeout := jwksFetchTimeout
	jwksFetchTimeout = 200 * time.Millisecond
	t.Cleanup(func() { jwksFetchTimeout = origTimeout })

	const bound = 5 * time.Second

	// Startup must not hang on a hung endpoint: it warns and proceeds.
	startupDone := make(chan *SupabaseJWTVerifier, 1)
	go func() {
		v, err := NewSupabaseJWTVerifier(context.Background(), server.URL, "https://test-project.supabase.co", "authenticated")
		if err != nil {
			t.Errorf("NewSupabaseJWTVerifier returned error: %v", err)
		}
		startupDone <- v
	}()

	var verifier *SupabaseJWTVerifier
	select {
	case verifier = <-startupDone:
	case <-time.After(bound):
		t.Fatalf("NewSupabaseJWTVerifier hung on a slow JWKS endpoint (no return within %s)", bound)
	}
	if verifier == nil {
		t.Fatal("verifier was nil")
	}

	// A request-time fetch must also return within the bound rather than
	// blocking on the stuck fetch worker.
	verifyDone := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), "any-token")
		verifyDone <- err
	}()

	select {
	case err := <-verifyDone:
		if err == nil {
			t.Fatal("expected an error from a hung JWKS endpoint, got nil")
		}
	case <-time.After(bound):
		t.Fatalf("Verify hung on a slow JWKS endpoint (no return within %s)", bound)
	}
}

func TestSupabaseJWTVerifier_MissingSub(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

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

	assertInvalidTokenReason(t, err, auth.ReasonClaimInvalidSUB)
}

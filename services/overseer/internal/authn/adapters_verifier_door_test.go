package authn_test

import (
	"altune/overseer/internal/authn"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

func TestJWKSThrottlesFetchesDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := jwksVerifier(srv)

	key := genKey(t)
	for i := 0; i < 50; i++ {
		token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Fatal("token verified during JWKS outage, want error")
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches during outage = %d, want 1 (throttled on attempt, not success)", got)
	}
}

func TestJWKSThrottlesConcurrentFloodDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := jwksVerifier(srv)

	key := genKey(t)
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
			if _, err := v.Verify(context.Background(), token); err == nil {
				t.Error("token verified during JWKS outage, want error")
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches during concurrent outage = %d, want 1 (throttled on attempt)", got)
	}
}

func TestJWKSRefetchesAfterSuccess(t *testing.T) {
	key := genKey(t)
	var hits int32
	base := jwksServer(t, "kid-1", key)
	defer base.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	v := jwksVerifier(srv)

	valid := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), valid); err != nil {
		t.Fatalf("valid token failed to verify: %v", err)
	}
	unknown := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), unknown); err == nil {
		t.Fatal("unknown-kid token verified, want error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches = %d, want 1 (cached after first success, unknown kid throttled)", got)
	}
}

func jwksServer(t *testing.T, kid string, key *ecdsa.PrivateKey) *httptest.Server {
	t.Helper()
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}
	const coord = 32
	x := base64.RawURLEncoding.EncodeToString(pub[1 : 1+coord])
	y := base64.RawURLEncoding.EncodeToString(pub[1+coord : 1+2*coord])
	doc := map[string]any{
		"keys": []map[string]any{
			{"kty": "EC", "crv": "P-256", "kid": kid, "x": x, "y": y, "alg": "ES256", "use": "sig"},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(doc)
	}))
}

func es256Token(t *testing.T, key *ecdsa.PrivateKey, kid string, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func genKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	return k
}

const (
	testIssuer   = "https://proj.supabase.co/auth/v1"
	testAudience = "authenticated"
)

func jwksVerifier(srv *httptest.Server) *authn.Verifier {
	return authn.New(srv.URL, testIssuer, "", srv.Client())
}

func validClaims(sub string) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   sub,
		Issuer:    testIssuer,
		Audience:  jwt.ClaimStrings{testAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
}

func TestVerifiesValidES256Token(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	token := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "owner-sub" {
		t.Errorf("subject = %q, want owner-sub", claims.Subject)
	}
}

func TestRejectsExpiredToken(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("owner-sub")
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expired token verified, want error")
	}
}

func TestRejectsKeyWhosePointIsOffTheCurve(t *testing.T) {
	key := genKey(t)
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}
	const coord = 32
	offCurveY := append([]byte(nil), pub[1+coord:]...)
	offCurveY[coord-1] ^= 1
	doc := map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "kid": "kid-1", "alg": "ES256", "use": "sig",
		"x": base64.RawURLEncoding.EncodeToString(pub[1 : 1+coord]),
		"y": base64.RawURLEncoding.EncodeToString(offCurveY),
	}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(doc)
	}))
	defer srv.Close()
	v := jwksVerifier(srv)

	token := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("token verified against an off-curve key, want error")
	}
}

func TestRejectsWrongSignature(t *testing.T) {
	srvKey := genKey(t)
	attackerKey := genKey(t)
	srv := jwksServer(t, "kid-1", srvKey)
	defer srv.Close()
	v := jwksVerifier(srv)

	token := es256Token(t, attackerKey, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("forged-signature token verified, want error")
	}
}

func TestRejectsAlgNone(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"owner-sub","exp":9999999999}`))
	token := header + "." + payload + "."
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("alg=none token verified, want error")
	}
}

func TestRejectsHS256WhenNoSecret(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims("owner-sub"))
	signed, err := tok.SignedString([]byte("attacker-guess"))
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("HS256 token verified with no secret configured, want error")
	}
}

func TestVerifiesHS256WhenSecretConfigured(t *testing.T) {
	secret := "legacy-hs256-secret"
	v := authn.New("http://unused.invalid/jwks", testIssuer, secret, http.DefaultClient)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims("owner-sub"))
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}
	claims, err := v.Verify(context.Background(), signed)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "owner-sub" {
		t.Errorf("subject = %q, want owner-sub", claims.Subject)
	}
}

func TestRejectsUnknownKid(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, authn.ErrNoKey) {
		t.Fatalf("unknown-kid token: err = %v, want ErrNoKey", err)
	}
}

func TestRejectsMissingSubject(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("")
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, authn.ErrNoSubject) {
		t.Fatalf("no-subject token: err = %v, want ErrNoSubject", err)
	}
}

func TestVerifiesTokenWithStringEncodedAudience(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	wireClaims := jwt.MapClaims{
		"sub": "owner-sub",
		"iss": testIssuer,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, wireClaims)
	tok.Header["kid"] = "kid-1"
	token, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "owner-sub" {
		t.Errorf("subject = %q, want owner-sub", claims.Subject)
	}
}

func TestRejectsForeignIssuer(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("owner-sub")
	claims.Issuer = "https://other-project.supabase.co/auth/v1"
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		t.Fatalf("Verify error = %v, want ErrTokenInvalidIssuer", err)
	}
}

func TestRejectsForeignAudience(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("owner-sub")
	claims.Audience = jwt.ClaimStrings{"service_role"}
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Fatalf("Verify error = %v, want ErrTokenInvalidAudience", err)
	}
}

func TestRejectsAbsentIssuerClaim(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("owner-sub")
	claims.Issuer = ""
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, jwt.ErrTokenRequiredClaimMissing) {
		t.Fatalf("Verify error = %v, want ErrTokenRequiredClaimMissing for the absent iss", err)
	}
}

func TestRejectsAbsentAudienceClaim(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := jwksVerifier(srv)

	claims := validClaims("owner-sub")
	claims.Audience = nil
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, jwt.ErrTokenRequiredClaimMissing) {
		t.Fatalf("Verify error = %v, want ErrTokenRequiredClaimMissing for the absent aud", err)
	}
}

func TestRejectsEverythingWithNoExpectedIssuer(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", "", srv.Client())

	token := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, authn.ErrNoIssuer) {
		t.Fatalf("Verify error = %v, want ErrNoIssuer", err)
	}
}

func TestRejectsGarbage(t *testing.T) {
	v := authn.New("http://unused.invalid/jwks", testIssuer, "", http.DefaultClient)
	for _, bad := range []string{"", "not-a-jwt", "a.b", "a.b.c.d"} {
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Errorf("garbage %q verified, want error", bad)
		}
	}
}

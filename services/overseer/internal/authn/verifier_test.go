package authn_test

import (
	"altune/overseer/internal/authn"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// jwksServer serves a JWKS with one ES256 key for the given kid, backed by key.
func jwksServer(t *testing.T, kid string, key *ecdsa.PrivateKey) *httptest.Server {
	t.Helper()
	// Encode the public point without touching the deprecated X/Y fields: Bytes()
	// yields the uncompressed SEC1 form (0x04 || X || Y), 65 bytes for P-256.
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

func validClaims(sub string) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   sub,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
}

// TestVerifiesValidES256Token: a JWKS-signed ES256 token verifies and yields its
// subject.
func TestVerifiesValidES256Token(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	token := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "owner-sub" {
		t.Errorf("subject = %q, want owner-sub", claims.Subject)
	}
}

// TestRejectsExpiredToken: an expired token fails verification.
func TestRejectsExpiredToken(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	claims := validClaims("owner-sub")
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expired token verified, want error")
	}
}

// TestRejectsWrongSignature: a token signed by a different key than the JWKS
// advertises fails.
func TestRejectsWrongSignature(t *testing.T) {
	srvKey := genKey(t)
	attackerKey := genKey(t)
	srv := jwksServer(t, "kid-1", srvKey)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	token := es256Token(t, attackerKey, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("forged-signature token verified, want error")
	}
}

// TestRejectsAlgNone is the classic downgrade attack: an unsigned alg=none token
// must never be accepted.
func TestRejectsAlgNone(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	// Build an alg=none token by hand: header.payload with an empty signature.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"owner-sub","exp":9999999999}`))
	token := header + "." + payload + "."
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("alg=none token verified, want error")
	}
}

// TestRejectsHS256WhenNoSecret is the algorithm-confusion guard: with no HS secret
// configured, an HS256 token (which an attacker could try to sign with the public
// key material) is rejected outright.
func TestRejectsHS256WhenNoSecret(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client()) // no HS secret

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims("owner-sub"))
	signed, err := tok.SignedString([]byte("attacker-guess"))
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("HS256 token verified with no secret configured, want error")
	}
}

// TestVerifiesHS256WhenSecretConfigured: a legacy HS256 project secret verifies
// HS256 tokens when explicitly configured.
func TestVerifiesHS256WhenSecretConfigured(t *testing.T) {
	secret := "legacy-hs256-secret"
	v := authn.New("http://unused.invalid/jwks", secret, http.DefaultClient)

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

// TestRejectsUnknownKid: a token whose kid is not in the JWKS fails closed.
func TestRejectsUnknownKid(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("unknown-kid token verified, want error")
	}
}

// TestRejectsMissingSubject: a verified token with no subject claim has no identity
// to allowlist and is rejected.
func TestRejectsMissingSubject(t *testing.T) {
	key := genKey(t)
	srv := jwksServer(t, "kid-1", key)
	defer srv.Close()
	v := authn.New(srv.URL, "", srv.Client())

	claims := validClaims("")
	token := es256Token(t, key, "kid-1", claims)
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("no-subject token verified, want error")
	}
}

// TestRejectsGarbage: a non-JWT string fails cleanly, never panics.
func TestRejectsGarbage(t *testing.T) {
	v := authn.New("http://unused.invalid/jwks", "", http.DefaultClient)
	for _, bad := range []string{"", "not-a-jwt", "a.b", "a.b.c.d"} {
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Errorf("garbage %q verified, want error", bad)
		}
	}
}

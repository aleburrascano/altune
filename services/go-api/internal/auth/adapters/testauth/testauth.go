// Package testauth is the NON-PRODUCTION test-auth path for go-api.
//
// It mints and verifies a self-signed token for a single dedicated test user so
// an automated driver can reach authenticated screens without the real Supabase
// web OAuth flow (see docs/webauth-testing-design.md). It is an intentional auth
// bypass, made safe by exactly one thing: it is constructed ONLY when
// config.TestAuthEnabled() is true. A production build never calls New, so it
// holds no signing key, its POST /test/login route is never mounted, and its
// tokens — signed with a key that only exists inside a non-prod process — are
// rejected by the real Supabase JWKS verifier.
//
// Two hard invariants keep it from becoming a real bypass even where it is
// wired:
//   - the signing key is generated per process with crypto/rand, never read
//     from config and never leaves the process, so a token cannot be forged
//     from the outside; and
//   - Verify only ever returns the fixed test identity, whatever the token's
//     sub claim says, so a test token can never impersonate a real account.
package testauth

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// testUserUUID is the single synthetic identity the test-auth path ever
// authenticates. It is a fixed, obviously-fake UUID, distinct from any real
// account and from shared.SystemUserId, so a test session can never be mistaken
// for a real user's signal.
var testUserUUID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// TestUserId returns the dedicated test identity the test-auth path issues and
// accepts tokens for, and nothing else.
func TestUserId() shared.UserId { return shared.NewUserId(testUserUUID) }

const (
	// tokenIssuer and tokenAudience scope test tokens to this path so they can
	// never be confused with a real Supabase token (which carries the project
	// URL as issuer and "authenticated" as audience).
	tokenIssuer   = "altune-test-auth"
	tokenAudience = "altune-test"

	// tokenLifetime bounds a minted token's validity, mirroring the real
	// verifier's short-lived-token posture.
	tokenLifetime = time.Hour

	// signingAlg is HMAC-SHA256: a symmetric MAC over the per-process random
	// key. It is deliberately NOT one of Supabase's asymmetric JWKS algorithms,
	// so the real verifier's key set can never validate a test token.
	signingAlg = jwa.HS256

	acceptableSkew = 5 * time.Second

	// signingKeyBytes is the HMAC key length (256 bits), matching the digest
	// size of SHA-256.
	signingKeyBytes = 32
)

// TestAuth issues and verifies test tokens under one per-process signing key.
// The same instance does both, so the key that signs a token is the only key
// that can verify it, and it exists only for this process's lifetime.
type TestAuth struct {
	key []byte
	now func() time.Time
}

// New builds a TestAuth with a freshly generated random signing key. Callers
// must only invoke it when config.TestAuthEnabled() is true; production never
// does, so no signing key is ever created there.
func New() (*TestAuth, error) {
	key := make([]byte, signingKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate test-auth signing key: %w", err)
	}
	return &TestAuth{key: key, now: time.Now}, nil
}

// IssueToken mints a signed test token for the dedicated test user and returns
// it with its expiry.
func (t *TestAuth) IssueToken() (string, time.Time, error) {
	now := t.now()
	exp := now.Add(tokenLifetime)
	tok, err := jwt.NewBuilder().
		Issuer(tokenIssuer).
		Audience([]string{tokenAudience}).
		Subject(testUserUUID.String()).
		IssuedAt(now).
		Expiration(exp).
		Build()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build test token: %w", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(signingAlg, t.key))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign test token: %w", err)
	}
	return string(signed), exp, nil
}

// Verify implements auth.TokenVerifier. It accepts only a token signed with this
// instance's key and carrying the test issuer/audience, and it always returns
// the dedicated test identity — never the sub the token carries — so a test
// token can never authenticate as a real user. Any failure maps to an
// *auth.InvalidTokenError (a 401), never a verifier-unavailable 503.
func (t *TestAuth) Verify(_ context.Context, tokenStr string) (auth.VerifiedToken, error) {
	tok, err := jwt.Parse(
		[]byte(tokenStr),
		jwt.WithKey(signingAlg, t.key),
		jwt.WithValidate(true),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithAcceptableSkew(acceptableSkew),
		jwt.WithClock(jwt.ClockFunc(t.now)),
	)
	if err != nil {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid, Detail: err.Error()}
	}
	// Defence in depth: reject any token whose sub is not the test user AND
	// return the fixed identity regardless, so even a mis-issued token can only
	// ever yield the test user, never a real account id.
	if tok.Subject() != testUserUUID.String() {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{
			Reason: auth.ReasonClaimInvalidSUB,
			Detail: "test token subject is not the dedicated test user",
		}
	}
	return auth.VerifiedToken{UserID: TestUserId(), ExpiresAt: tok.Expiration()}, nil
}

// Combine returns a verifier that accepts a valid test token OR a real token.
// The test verifier runs first and cheaply (a local MAC check, no network); only
// when it rejects does the real Supabase verifier run, so real tokens keep their
// exact existing behaviour, including the JWKS-unavailable 503 path. It is used
// only in non-prod, where both verifiers are present.
func Combine(test, fallback auth.TokenVerifier) auth.TokenVerifier {
	return auth.VerifierFunc(func(ctx context.Context, token string) (auth.VerifiedToken, error) {
		if verified, err := test.Verify(ctx, token); err == nil {
			return verified, nil
		}
		return fallback.Verify(ctx, token)
	})
}

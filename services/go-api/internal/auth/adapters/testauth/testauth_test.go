package testauth

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func mustNew(t *testing.T) *TestAuth {
	t.Helper()
	ta, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ta
}

func TestVerify_AcceptsIssuedTokenAsTestUser(t *testing.T) {
	ta := mustNew(t)
	token, exp, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if !exp.After(time.Now()) {
		t.Fatalf("expiry %v is not in the future", exp)
	}

	verified, err := ta.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify rejected a freshly issued token: %v", err)
	}
	if verified.UserID != TestUserId() {
		t.Errorf("Verify returned %s, want the dedicated test user %s", verified.UserID, TestUserId())
	}
}

// The test identity must never collide with a real account marker.
func TestTestUser_IsDistinctSyntheticIdentity(t *testing.T) {
	if TestUserId() == shared.SystemUserId() {
		t.Error("test user id collides with the system user id")
	}
	if TestUserId().IsZero() {
		t.Error("test user id is the zero UUID")
	}
}

func TestVerify_RejectsForeignKey(t *testing.T) {
	issuer := mustNew(t)
	token, _, err := issuer.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	other := mustNew(t) // independent random key
	_, err = other.Verify(context.Background(), token)
	assertInvalidToken(t, err)
}

func TestVerify_RejectsExpiredToken(t *testing.T) {
	ta := mustNew(t)
	ta.now = func() time.Time { return time.Unix(1_000_000, 0) }
	token, _, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// Advance well past expiry + skew.
	ta.now = func() time.Time { return time.Unix(1_000_000, 0).Add(tokenLifetime + time.Hour) }
	_, err = ta.Verify(context.Background(), token)
	assertInvalidToken(t, err)
}

// A token signed with the real key but claiming a different sub must never
// authenticate as that sub — the test path only ever yields the test user.
func TestVerify_RejectsForgedRealUserSub(t *testing.T) {
	ta := mustNew(t)
	realUser := uuid.New().String()
	now := time.Now()
	tok, err := jwt.NewBuilder().
		Issuer(tokenIssuer).
		Audience([]string{tokenAudience}).
		Subject(realUser).
		IssuedAt(now).
		Expiration(now.Add(tokenLifetime)).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(signingAlg, ta.key))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	verified, err := ta.Verify(context.Background(), string(signed))
	if err == nil {
		t.Fatalf("Verify accepted a token claiming sub=%s, returned %s", realUser, verified.UserID)
	}
	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) || tokenErr.Reason != auth.ReasonClaimInvalidSUB {
		t.Fatalf("want ReasonClaimInvalidSUB, got %v", err)
	}
}

func TestVerify_RejectsWrongIssuerAndAudience(t *testing.T) {
	ta := mustNew(t)
	now := time.Now()
	for _, tc := range []struct {
		name     string
		iss, aud string
	}{
		{"wrong issuer", "evil", tokenAudience},
		{"wrong audience", tokenIssuer, "authenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok, _ := jwt.NewBuilder().
				Issuer(tc.iss).
				Audience([]string{tc.aud}).
				Subject(testUserUUID.String()).
				IssuedAt(now).
				Expiration(now.Add(tokenLifetime)).
				Build()
			signed, err := jwt.Sign(tok, jwt.WithKey(signingAlg, ta.key))
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			_, err = ta.Verify(context.Background(), string(signed))
			assertInvalidToken(t, err)
		})
	}
}

func TestVerify_RejectsGarbage(t *testing.T) {
	ta := mustNew(t)
	for _, s := range []string{"", "not-a-jwt", "a.b.c"} {
		if _, err := ta.Verify(context.Background(), s); err == nil {
			t.Errorf("Verify accepted garbage %q", s)
		}
	}
}

// A different HMAC algorithm over the same bytes must not verify: the parse
// pins HS256.
func TestVerify_RejectsWrongAlg(t *testing.T) {
	ta := mustNew(t)
	now := time.Now()
	tok, _ := jwt.NewBuilder().
		Issuer(tokenIssuer).Audience([]string{tokenAudience}).
		Subject(testUserUUID.String()).IssuedAt(now).Expiration(now.Add(tokenLifetime)).Build()
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.HS512, ta.key))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := ta.Verify(context.Background(), string(signed)); err == nil {
		t.Error("Verify accepted an HS512 token where HS256 was required")
	}
}

func TestCombine_AcceptsTestTokenWithoutCallingReal(t *testing.T) {
	ta := mustNew(t)
	token, _, _ := ta.IssueToken()

	realCalled := false
	fallback := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		realCalled = true
		return auth.VerifiedToken{}, errors.New("real should not run")
	})

	verified, err := Combine(ta, fallback).Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Combine rejected a valid test token: %v", err)
	}
	if verified.UserID != TestUserId() {
		t.Errorf("got %s, want test user", verified.UserID)
	}
	if realCalled {
		t.Error("real verifier ran even though the test token was valid")
	}
}

func TestCombine_FallsThroughToRealForNonTestToken(t *testing.T) {
	ta := mustNew(t)
	want := shared.NewUserId(uuid.New())
	fallback := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{UserID: want, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})

	verified, err := Combine(ta, fallback).Verify(context.Background(), "some.supabase.token")
	if err != nil {
		t.Fatalf("Combine: %v", err)
	}
	if verified.UserID != want {
		t.Errorf("got %s, want real user %s from the fallthrough", verified.UserID, want)
	}
}

func assertInvalidToken(t *testing.T, err error) {
	t.Helper()
	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected *auth.InvalidTokenError, got %T: %v", err, err)
	}
}

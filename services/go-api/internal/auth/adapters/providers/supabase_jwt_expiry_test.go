package providers

import (
	"altune/go-api/internal/auth"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func TestSupabaseJWTVerifier_VerifyReportsTokenExpiry(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	sub := uuid.New().String()
	exp := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": exp,
		"iat": time.Now().Add(-1 * time.Minute),
	})

	verified, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
	if !verified.ExpiresAt.Equal(exp) {
		t.Errorf("expiry: got %v, want the token's exp %v", verified.ExpiresAt, exp)
	}
}

func TestVerifiedToken_RejectsTokenWithoutExpiry(t *testing.T) {
	token, err := jwt.NewBuilder().Subject(uuid.New().String()).Build()
	if err != nil {
		t.Fatalf("build token: %v", err)
	}

	_, err = verifiedToken(token)

	var invalid *auth.InvalidTokenError
	if !errors.As(err, &invalid) || invalid.Reason != auth.ReasonClaimMissingEXP {
		t.Fatalf("err = %v, want InvalidTokenError with reason %q", err, auth.ReasonClaimMissingEXP)
	}
}

package providers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSupabaseJWTVerifier_VerifyExpiringReportsTokenExpiry(t *testing.T) {
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

	verified, err := verifier.VerifyExpiring(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyExpiring: %v", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
	if !verified.ExpiresAt.Equal(exp) {
		t.Errorf("expiry: got %v, want the token's exp %v", verified.ExpiresAt, exp)
	}
}

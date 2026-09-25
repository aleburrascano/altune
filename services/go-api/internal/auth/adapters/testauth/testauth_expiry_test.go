package testauth

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type expiringFallback struct {
	verified auth.VerifiedToken
}

func (f expiringFallback) Verify(ctx context.Context, token string) (shared.UserId, error) {
	verified, err := f.VerifyExpiring(ctx, token)
	return verified.UserID, err
}

func (f expiringFallback) VerifyExpiring(context.Context, string) (auth.VerifiedToken, error) {
	return f.verified, nil
}

func TestVerifyExpiring_ReportsIssuedTokenExpiry(t *testing.T) {
	ta := mustNew(t)
	token, exp, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	verified, err := ta.VerifyExpiring(context.Background(), token)
	if err != nil {
		t.Fatalf("VerifyExpiring: %v", err)
	}
	if !verified.ExpiresAt.Equal(exp.Truncate(time.Second)) {
		t.Errorf("expiry: got %v, want %v", verified.ExpiresAt, exp)
	}
}

func TestCombine_TestTokenKeepsItsExpiry(t *testing.T) {
	ta := mustNew(t)
	token, exp, _ := ta.IssueToken()
	fallback := auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
		return shared.UserId{}, errors.New("real should not run")
	})

	verified, err := auth.VerifyToken(context.Background(), Combine(ta, fallback), token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if !verified.ExpiresAt.Equal(exp.Truncate(time.Second)) {
		t.Errorf("expiry through Combine: got %v, want %v", verified.ExpiresAt, exp)
	}
}

func TestCombine_RealTokenKeepsItsExpiry(t *testing.T) {
	ta := mustNew(t)
	want := auth.VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}

	verified, err := auth.VerifyToken(context.Background(), Combine(ta, expiringFallback{want}), "some.supabase.token")
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if verified != want {
		t.Errorf("got %+v, want the real verifier's %+v", verified, want)
	}
}

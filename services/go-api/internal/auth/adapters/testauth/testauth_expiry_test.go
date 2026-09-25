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

func TestVerify_ReportsIssuedTokenExpiry(t *testing.T) {
	ta := mustNew(t)
	token, exp, err := ta.IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	verified, err := ta.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !verified.ExpiresAt.Equal(exp.Truncate(time.Second)) {
		t.Errorf("expiry: got %v, want %v", verified.ExpiresAt, exp)
	}
}

func TestCombine_TestTokenKeepsItsExpiry(t *testing.T) {
	ta := mustNew(t)
	token, exp, _ := ta.IssueToken()
	fallback := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{}, errors.New("real should not run")
	})

	verified, err := Combine(ta, fallback).Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !verified.ExpiresAt.Equal(exp.Truncate(time.Second)) {
		t.Errorf("expiry through Combine: got %v, want %v", verified.ExpiresAt, exp)
	}
}

func TestCombine_RealTokenKeepsItsExpiry(t *testing.T) {
	ta := mustNew(t)
	want := auth.VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: time.Now().Add(time.Hour)}

	fallback := auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
		return want, nil
	})

	verified, err := Combine(ta, fallback).Verify(context.Background(), "some.supabase.token")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified != want {
		t.Errorf("got %+v, want the real verifier's %+v", verified, want)
	}
}

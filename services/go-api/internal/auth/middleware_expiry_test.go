package auth

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func verifies(verified VerifiedToken) VerifierFunc {
	return func(context.Context, string) (VerifiedToken, error) {
		return verified, nil
	}
}

func serveThroughMiddleware(t *testing.T, verifier TokenVerifier) (time.Time, bool) {
	t.Helper()
	var expiresAt time.Time
	var known bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		expiresAt, known = TokenExpiryFromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	Middleware(verifier)(next).ServeHTTP(httptest.NewRecorder(), req)
	return expiresAt, known
}

func TestMiddleware_TokenExpiryReachesHandler(t *testing.T) {
	want := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	verifier := verifies(VerifiedToken{UserID: shared.NewUserId(uuid.New()), ExpiresAt: want})

	got, known := serveThroughMiddleware(t, verifier)

	if !known || !got.Equal(want) {
		t.Fatalf("token expiry on context = %v (known %v), want %v", got, known, want)
	}
}

func TestMiddleware_RejectsVerifiedTokenWithoutExpiry(t *testing.T) {
	next, called := noopHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	Middleware(verifies(VerifiedToken{UserID: shared.NewUserId(uuid.New())}))(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token with no expiry", rec.Code)
	}
	if *called {
		t.Fatal("handler ran for a token with no expiry")
	}
	if reason := decodeRejectBody(t, rec)["reason"]; reason != string(ReasonClaimMissingEXP) {
		t.Fatalf("reason = %q, want %q", reason, ReasonClaimMissingEXP)
	}
}

func TestUntilTokenExpiry_EndsAtExpiryWithCause(t *testing.T) {
	parent := ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if cause := context.Cause(ctx); !errors.Is(cause, ErrTokenExpired) {
		t.Fatalf("cause = %v, want ErrTokenExpired", cause)
	}
}

func TestUntilTokenExpiry_UnknownExpiryEndsOnlyWithParent(t *testing.T) {
	ctx, cancel := UntilTokenExpiry(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context with no token expiry ended on its own")
	case <-time.After(50 * time.Millisecond):
	}
}

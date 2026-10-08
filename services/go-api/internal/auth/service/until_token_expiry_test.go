package service

import (
	"altune/go-api/internal/auth/domain"
	"context"
	"errors"
	"testing"
	"time"
)

func TestUntilTokenExpiry_EndsAtExpiryWithCause(t *testing.T) {
	parent := ContextWithTokenExpiry(context.Background(), time.Now().Add(20*time.Millisecond))

	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if cause := context.Cause(ctx); !errors.Is(cause, domain.ErrTokenExpired) {
		t.Fatalf("cause = %v, want domain.ErrTokenExpired", cause)
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

	cancel()
	<-ctx.Done()
}

func TestUntilTokenExpiry_UnknownExpiryEndsWithParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived its parent")
	}
	if errors.Is(context.Cause(ctx), domain.ErrTokenExpired) {
		t.Fatal("parent cancellation reported as token expiry")
	}
}

func TestUntilTokenExpiry_ExpiryPassingBetweenTicks(t *testing.T) {
	parent := ContextWithTokenExpiry(context.Background(), time.Now().Add(60*time.Millisecond))
	ctx, cancel := UntilTokenExpiry(parent)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context ended before the token expiry")
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context outlived the token expiry")
	}
	if !errors.Is(context.Cause(ctx), domain.ErrTokenExpired) {
		t.Fatalf("cause = %v, want domain.ErrTokenExpired", context.Cause(ctx))
	}
}

func TestErrTokenExpired_LookAlikeIsNotTheSentinel(t *testing.T) {
	if errors.Is(errors.New("auth: token expired"), domain.ErrTokenExpired) {
		t.Fatal("an error with the same text matched the sentinel")
	}
}

package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"testing"
)

func TestStaticTokenSourceReturnsToken(t *testing.T) {
	got, err := goapi.StaticTokenSource("op-token").Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "op-token" {
		t.Fatalf("Token = %q, want op-token", got)
	}
}

func TestStaticTokenSourceEmptyFailsClosed(t *testing.T) {
	_, err := goapi.StaticTokenSource("").Token(context.Background())
	if !errors.Is(err, goapi.ErrNoToken) {
		t.Fatalf("empty token error = %v, want ErrNoToken", err)
	}
}

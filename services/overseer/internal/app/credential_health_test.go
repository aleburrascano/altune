package app

import (
	"altune/overseer/internal/goapi"
	"testing"
)

func TestCredentialHealthIsWiredForARefreshingSource(t *testing.T) {
	tokens, err := goapi.NewRefreshingTokenSource("https://x.supabase.co", "anon-key", "seed-refresh",
		goapi.WithPasswordGrant("readonly@altune.test", "password"))
	if err != nil {
		t.Fatalf("NewRefreshingTokenSource: %v", err)
	}

	health := credentialHealth(tokens)

	if health == nil {
		t.Fatal("a refreshing source wired no credential health")
	}
	if !health().PasswordGrant {
		t.Error("credential health is not the refreshing source's own state")
	}
}

func TestCredentialHealthIsAbsentForAStaticSource(t *testing.T) {
	if credentialHealth(goapi.StaticTokenSource("static-jwt")) != nil {
		t.Error("a static token has no refresh state to report, yet credential health was wired")
	}
}

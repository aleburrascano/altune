package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const (
	envSupabaseURL                = "OVERSEER_SUPABASE_URL"
	envSupabaseAnon               = "OVERSEER_SUPABASE_ANON_KEY"
	envReadOnlyRefreshToken       = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN"
	envReadOnlyRefreshFile        = "OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE"
	envReadOnlyToken              = "OVERSEER_GOAPI_READONLY_TOKEN"
	envReadOnlyEmail              = "OVERSEER_GOAPI_READONLY_EMAIL"
	envReadOnlyPassword           = "OVERSEER_GOAPI_READONLY_PASSWORD"
	envLegacyOperatorRefreshToken = "OVERSEER_GOAPI_REFRESH_TOKEN"
	envLegacyOperatorToken        = "OVERSEER_GOAPI_TOKEN"
)

func isNullSource(src goapi.TokenSource) bool {
	switch src.(type) {
	case *goapihttp.RefreshingTokenSource, goapi.StaticTokenSource:
		return false
	}
	_, err := src.Token(context.Background())
	return errors.Is(err, goapi.ErrNoToken)
}

func TestSelectTokenSource(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "refresh_token")
	refreshEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyRefreshToken:
			return "refresh"
		case envReadOnlyRefreshFile:
			return persistPath
		default:
			return ""
		}
	}
	if _, ok := goapihttp.SelectTokenSource(refreshEnv).(*goapihttp.RefreshingTokenSource); !ok {
		t.Fatal("all refresh vars set should select RefreshingTokenSource")
	}

	staticEnv := func(k string) string {
		if k == envReadOnlyToken {
			return "static-readonly-token"
		}
		return ""
	}
	if got, ok := goapihttp.SelectTokenSource(staticEnv).(goapi.StaticTokenSource); !ok || string(got) != "static-readonly-token" {
		t.Fatalf("only static token set should select StaticTokenSource, got %T", goapihttp.SelectTokenSource(staticEnv))
	}

	if src := goapihttp.SelectTokenSource(func(string) string { return "" }); !isNullSource(src) {
		t.Fatalf("no vars set should select the null source, got %T", src)
	}

	badURLEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "://bad url"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyRefreshToken:
			return "refresh"
		default:
			return ""
		}
	}
	if src := goapihttp.SelectTokenSource(badURLEnv); !isNullSource(src) {
		t.Fatalf("refresh vars set but malformed URL should fail closed to the null source, got %T", src)
	}
}

func TestSelectTokenSource_NoOperatorFallback(t *testing.T) {
	operatorEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envLegacyOperatorRefreshToken:
			return "operator-refresh"
		case envLegacyOperatorToken:
			return "operator-bearer"
		default:
			return ""
		}
	}
	if src := goapihttp.SelectTokenSource(operatorEnv); !isNullSource(src) {
		t.Fatalf("operator credentials must not be adopted, got %T", src)
	}
}

func TestRefreshTokenPathIsPrincipalScoped(t *testing.T) {
	const operatorPath = "/var/lib/overseer/refresh_token"

	if got := goapihttp.RefreshTokenPath(func(string) string { return "" }); got == operatorPath {
		t.Fatalf("default persistence path = %q, the operator's own file", got)
	}
	override := func(k string) string {
		if k == envReadOnlyRefreshFile {
			return "/tmp/overseer-readonly-token"
		}
		return ""
	}
	if got := goapihttp.RefreshTokenPath(override); got != "/tmp/overseer-readonly-token" {
		t.Errorf("path override ignored: got %q", got)
	}
}

func TestNullTokenSourceFailsClosed(t *testing.T) {
	_, err := goapihttp.SelectTokenSource(func(string) string { return "" }).Token(context.Background())
	if !errors.Is(err, goapi.ErrNoToken) {
		t.Fatalf("null source error = %v, want ErrNoToken", err)
	}
}

func TestSelectTokenSourceRefreshesWithCredentialsAndNoSeed(t *testing.T) {
	persistPath := rtsTokenFile(t).Path
	credentialsEnv := func(k string) string {
		switch k {
		case envSupabaseURL:
			return "https://ref.supabase.co"
		case envSupabaseAnon:
			return "anon"
		case envReadOnlyEmail:
			return pgEmail
		case envReadOnlyPassword:
			return pgPassword
		case envReadOnlyRefreshFile:
			return persistPath
		default:
			return ""
		}
	}
	src, ok := goapihttp.SelectTokenSource(credentialsEnv).(*goapihttp.RefreshingTokenSource)
	if !ok {
		t.Fatalf("credentials without a seed should select RefreshingTokenSource, got %T", goapihttp.SelectTokenSource(credentialsEnv))
	}
	if !src.Health().PasswordGrant {
		t.Fatal("SelectTokenSource did not pass the password grant to the source")
	}

	halfEnv := func(k string) string {
		if k == envReadOnlyPassword {
			return ""
		}
		return credentialsEnv(k)
	}
	if !isNullSource(goapihttp.SelectTokenSource(halfEnv)) {
		t.Fatal("an email without a password and no seed must stay source-down")
	}
}

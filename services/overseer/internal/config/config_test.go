package config_test

import (
	"altune/overseer/internal/config"
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

const (
	ownerUserID = "00000000-0000-0000-0000-000000000001"
	supaURL     = "https://proj.supabase.co"
	anonKey     = "public-anon-key"
	jwtSecret   = "super-secret-hs256-signing-key-value"
)

// validEnv is the minimal set that lets Load succeed.
func validEnv() map[string]string {
	return map[string]string{
		"OVERSEER_OWNER_USER_ID":     ownerUserID,
		"OVERSEER_SUPABASE_URL":      supaURL,
		"OVERSEER_SUPABASE_ANON_KEY": anonKey,
	}
}

func TestLoadRejectsMissingOwnerUserID(t *testing.T) {
	env := validEnv()
	env["OVERSEER_OWNER_USER_ID"] = ""
	setEnv(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error with no owner user id, got nil")
	}
}

func TestLoadRejectsMissingSupabaseURL(t *testing.T) {
	env := validEnv()
	env["OVERSEER_SUPABASE_URL"] = ""
	setEnv(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error with no supabase url, got nil")
	}
}

func TestLoadRejectsMissingAnonKey(t *testing.T) {
	env := validEnv()
	env["OVERSEER_SUPABASE_ANON_KEY"] = ""
	setEnv(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error with no anon key, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, validEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8090 {
		t.Errorf("Port = %d, want default 8090", cfg.Port)
	}
	if !cfg.IsDevelopment() {
		t.Error("expected development env by default")
	}
	if cfg.TickInterval <= 0 {
		t.Errorf("TickInterval = %v, want positive default", cfg.TickInterval)
	}
	if cfg.OwnerUserID != ownerUserID {
		t.Errorf("OwnerUserID = %q, want %q", cfg.OwnerUserID, ownerUserID)
	}
}

// JWKSURL derives from the Supabase URL by default, and honors an explicit override.
func TestJWKSURL(t *testing.T) {
	setEnv(t, validEnv())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.JWKSURL(), supaURL+"/auth/v1/.well-known/jwks.json"; got != want {
		t.Errorf("JWKSURL = %q, want %q", got, want)
	}

	env := validEnv()
	env["OVERSEER_SUPABASE_JWKS_URL"] = "https://override.example/jwks"
	setEnv(t, env)
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.JWKSURL(); got != "https://override.example/jwks" {
		t.Errorf("JWKSURL override = %q, want the explicit value", got)
	}
}

// IssuerURL is the GoTrue issuer derived from the project URL, and a JWKS override
// (where keys come from) must not move it (who signed the token).
func TestIssuerURL(t *testing.T) {
	env := validEnv()
	env["OVERSEER_SUPABASE_URL"] = supaURL + "/"
	env["OVERSEER_SUPABASE_JWKS_URL"] = "https://override.example/jwks"
	setEnv(t, env)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.IssuerURL(), supaURL+"/auth/v1"; got != want {
		t.Errorf("IssuerURL = %q, want %q", got, want)
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	env := validEnv()
	env["OVERSEER_PORT"] = "70000"
	setEnv(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestLoadRejectsBadTick(t *testing.T) {
	env := validEnv()
	env["OVERSEER_TICK_INTERVAL"] = "-1s"
	setEnv(t, env)
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for non-positive tick interval")
	}
}

// OVERSEER_BASE_PATH is normalized to a safe outbound prefix.
func TestLoadNormalizesBasePath(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unset default", "", ""},
		{"whitespace collapses to empty", "   ", ""},
		{"clean value kept", "/overseer", "/overseer"},
		{"trailing slash trimmed", "/overseer/", "/overseer"},
		{"missing leading slash added", "overseer", "/overseer"},
		{"nested trailing slash trimmed", "/a/b/", "/a/b"},
		{"bare slash collapses to empty", "/", ""},
		{"double slash collapses to empty", "//", ""},
		{"protocol-relative leading collapsed", "//evil.example", "/evil.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env["OVERSEER_BASE_PATH"] = tc.raw
			setEnv(t, env)
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.BasePath != tc.want {
				t.Errorf("BasePath for %q = %q, want %q", tc.raw, cfg.BasePath, tc.want)
			}
		})
	}
}

// LogValue must never leak the HS256 JWT secret.
func TestLogValueRedactsJWTSecret(t *testing.T) {
	env := validEnv()
	env["OVERSEER_SUPABASE_JWT_SECRET"] = jwtSecret
	setEnv(t, env)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.LogValue().String(); strings.Contains(got, jwtSecret) {
		t.Errorf("LogValue leaked the JWT secret: %s", got)
	}
}

package config

import (
	"strings"
	"testing"
)

// The JWKS endpoint is the trust root for every bearer-token signature check,
// so a plaintext URL would let a network-positioned attacker substitute the key
// set and mint tokens that verify (#1030).
func TestLoad_SupabaseURLsRejectPlaintext(t *testing.T) {
	fields := []string{"SUPABASE_JWT_JWKS_URL", "SUPABASE_PROJECT_URL"}
	urls := []struct {
		name string
		url  string
	}{
		{name: "http remote host", url: "http://example.supabase.co/auth/v1/.well-known/jwks.json"},
		{name: "http upper-case scheme", url: "HTTP://example.supabase.co/auth/v1/.well-known/jwks.json"},
		{name: "http userinfo spoofing loopback", url: "http://127.0.0.1@example.supabase.co/jwks"},
		{name: "http loopback-looking subdomain", url: "http://localhost.example.com/jwks"},
		{name: "ftp", url: "ftp://example.supabase.co/jwks"},
		{name: "ws", url: "ws://example.supabase.co/jwks"},
	}
	for _, field := range fields {
		for _, tt := range urls {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				setEnv(t, validConfigEnv(map[string]string{field: tt.url}))

				_, err := Load()
				if err == nil {
					t.Fatalf("expected %s=%q to be rejected as non-https", field, tt.url)
				}
				if !strings.Contains(err.Error(), field) || !strings.Contains(err.Error(), "https") {
					t.Errorf("error should name %s and the https requirement, got: %v", field, err)
				}
			})
		}
	}
}

// Loopback traffic never crosses a network an attacker can sit on, so local
// Supabase (supabase start serves http://127.0.0.1:54321) stays usable.
func TestLoad_SupabaseURLsAllowLoopbackHTTP(t *testing.T) {
	for _, base := range []string{
		"http://127.0.0.1:54321",
		"http://localhost:54321",
		"http://LOCALHOST:54321",
		"http://[::1]:54321",
	} {
		t.Run(base, func(t *testing.T) {
			setEnv(t, validConfigEnv(map[string]string{
				"SUPABASE_PROJECT_URL":  base,
				"SUPABASE_JWT_JWKS_URL": base + "/auth/v1/.well-known/jwks.json",
			}))

			if _, err := Load(); err != nil {
				t.Fatalf("loopback http should be accepted, got: %v", err)
			}
		})
	}
}

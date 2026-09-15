package providers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The JWKS fetch is the trust root for Verify: a plaintext JWKS URL lets a
// network-positioned attacker substitute the key set (#1030).
func TestNewSupabaseJWTVerifier_RejectsPlaintextJWKSURL(t *testing.T) {
	for _, raw := range []string{
		"http://test-project.supabase.co/auth/v1/.well-known/jwks.json",
		"http://127.0.0.1@test-project.supabase.co/jwks",
		"ftp://test-project.supabase.co/jwks",
		"test-project.supabase.co/jwks",
	} {
		t.Run(raw, func(t *testing.T) {
			v, err := NewSupabaseJWTVerifier(context.Background(), raw, "https://test-project.supabase.co", "authenticated")
			if err == nil || v != nil {
				t.Fatalf("NewSupabaseJWTVerifier(%q) = %v, %v; want nil verifier and error", raw, v, err)
			}
			if !strings.Contains(err.Error(), "https") {
				t.Errorf("error should name the https requirement, got: %v", err)
			}
		})
	}
}

func TestRequireSecureJWKSURL_AllowsHTTPSAndLoopbackHTTP(t *testing.T) {
	for _, raw := range []string{
		"https://test-project.supabase.co/auth/v1/.well-known/jwks.json",
		"http://127.0.0.1:54321/auth/v1/.well-known/jwks.json",
		"http://localhost:54321/jwks",
		"http://[::1]:54321/jwks",
	} {
		t.Run(raw, func(t *testing.T) {
			if err := requireSecureJWKSURL(raw); err != nil {
				t.Fatalf("requireSecureJWKSURL(%q) = %v; want nil", raw, err)
			}
		})
	}
}

// An https JWKS endpoint (or anything in front of it) must not be able to
// downgrade the fetch to plaintext with a redirect.
func TestCheckJWKSRedirect_RefusesPlaintextDowngrade(t *testing.T) {
	mustReq := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return &http.Request{URL: u}
	}
	via := []*http.Request{mustReq("https://test-project.supabase.co/auth/v1/.well-known/jwks.json")}

	if err := checkJWKSRedirect(mustReq("http://attacker.example/jwks"), via); err == nil {
		t.Fatal("redirect to plaintext remote host was followed")
	}
	if err := checkJWKSRedirect(mustReq("https://cdn.supabase.co/jwks"), via); err != nil {
		t.Fatalf("redirect to https should be followed, got: %v", err)
	}

	long := make([]*http.Request, 10)
	for i := range long {
		long[i] = via[0]
	}
	if err := checkJWKSRedirect(mustReq("https://cdn.supabase.co/jwks"), long); err == nil {
		t.Fatal("redirect chain limit not enforced")
	}
}

package providers_test

import (
	"altune/go-api/internal/platform/auth"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

const maxJWKSBody = 1 << 20

func TestNewSupabaseJWTVerifier_RejectsPlaintextJWKSURL(t *testing.T) {
	for _, raw := range []string{
		"http://test-project.supabase.co/auth/v1/.well-known/jwks.json",
		"http://127.0.0.1@test-project.supabase.co/jwks",
		"ftp://test-project.supabase.co/jwks",
		"test-project.supabase.co/jwks",
	} {
		t.Run(raw, func(t *testing.T) {
			v, err := auth.NewSupabaseJWTVerifier(t.Context(), raw, testProjectURL, testAudience, time.Now, auth.NoopAuthMetrics(), auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule())
			if err == nil || v != nil {
				t.Fatalf("NewSupabaseJWTVerifier(%q) = %v, %v; want nil verifier and error", raw, v, err)
			}
			if !strings.Contains(err.Error(), "https") {
				t.Errorf("error should name the https requirement, got: %v", err)
			}
		})
	}
}

func TestNewSupabaseJWTVerifier_AllowsHTTPSAndLoopbackHTTP(t *testing.T) {
	for _, raw := range []string{
		"https://test-project.supabase.co/auth/v1/.well-known/jwks.json",
		"http://127.0.0.1:54321/auth/v1/.well-known/jwks.json",
		"http://localhost:54321/jwks",
		"http://[::1]:54321/jwks",
	} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if v, err := auth.NewSupabaseJWTVerifier(ctx, raw, testProjectURL, testAudience, time.Now, auth.NoopAuthMetrics(), auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule()); err != nil || v == nil {
				t.Fatalf("NewSupabaseJWTVerifier(%q) = %v, %v; want a verifier", raw, v, err)
			}
		})
	}
}

func redirectingServer(t *testing.T, target func(*http.Request) string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, target(r), http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestJWKSRedirect_RefusesPlaintextDowngrade(t *testing.T) {
	server, _ := redirectingServer(t, func(*http.Request) string { return "http://attacker.example/jwks" })
	verifier := newVerifier(t, server.URL, time.Now)

	_, err := verifier.Verify(t.Context(), "any-token")

	assertErrorSays(t, err, "refusing JWKS redirect")
	assertErrorSays(t, err, `host "attacker.example"`)
}

func TestJWKSRedirect_FollowsASecureTarget(t *testing.T) {
	f := newTestJWTFixture(t)
	server, _ := redirectingServer(t, func(*http.Request) string { return f.jwksServer.URL })
	verifier := newVerifier(t, server.URL, time.Now)

	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("Verify through a redirect to a secure JWKS host: %v", err)
	}
}

func TestJWKSRedirect_ChainIsBounded(t *testing.T) {
	server, hits := redirectingServer(t, func(r *http.Request) string { return "http://" + r.Host + r.URL.Path + "x" })
	verifier := newVerifier(t, server.URL+"/", time.Now)
	hits.Store(0)

	_, err := verifier.Verify(t.Context(), "any-token")

	assertErrorSays(t, err, "stopped after 10 JWKS redirects")
	if got := hits.Load(); got != 10 {
		t.Fatalf("requests in one redirect chain: got %d, want 10", got)
	}
}

func marshalKeySet(t *testing.T, pub *rsa.PublicKey, kid string) []byte {
	t.Helper()
	set := jwk.NewSet()
	_ = set.AddKey(signingJWK(t, *pub, kid))
	body, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal key set: %v", err)
	}
	return body
}

func paddedKeySet(t *testing.T, f *testJWTFixture, size int) []byte {
	t.Helper()
	body := marshalKeySet(t, &f.privateKey.PublicKey, f.keyID)
	return append(bytes.Repeat([]byte{' '}, size-len(body)), body...)
}

func servingJWKS(t *testing.T, body []byte, gzipped bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if gzipped {
			w.Header().Set("Content-Encoding", "gzip")
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestCappedJWKSBody_ReadsUpToTheCapAndFailsPastIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		ok   bool
	}{
		{"one byte under the cap", maxJWKSBody - 1, true},
		{"exactly at the cap", maxJWKSBody, true},
		{"one byte over the cap", maxJWKSBody + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newRotatingFixture(t)
			verifier := newVerifier(t, servingJWKS(t, paddedKeySet(t, f, tc.size), false), time.Now)

			_, err := verifier.Verify(t.Context(), f.validToken(t))

			if tc.ok && err != nil {
				t.Fatalf("a %d-byte key set: %v", tc.size, err)
			}
			if !tc.ok {
				assertErrorIs(t, err, auth.ErrJWKSBodyTooLarge)
			}
		})
	}
}

func TestCappedJWKSBodyTransport_CapsDecompressedBytes(t *testing.T) {
	f, _ := newRotatingFixture(t)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(paddedKeySet(t, f, maxJWKSBody+1)); err != nil {
		t.Fatalf("compress body: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close compressed body: %v", err)
	}
	verifier := newVerifier(t, servingJWKS(t, compressed.Bytes(), true), time.Now)

	_, err := verifier.Verify(t.Context(), f.validToken(t))

	assertErrorIs(t, err, auth.ErrJWKSBodyTooLarge)
}

func TestSupabaseJWTVerifier_OversizedJWKSBodyKeepsCachedKeys(t *testing.T) {
	f, _ := newRotatingFixture(t)
	keyB := generateRSAKey(t)
	oversized := append(marshalKeySet(t, &keyB.PublicKey, "key-b"), bytes.Repeat([]byte{' '}, maxJWKSBody)...)
	withinCap := marshalKeySet(t, &f.privateKey.PublicKey, f.keyID)
	var serveOversized atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if serveOversized.Load() {
			_, _ = w.Write(oversized)
			return
		}
		_, _ = w.Write(withinCap)
	}))
	t.Cleanup(server.Close)
	metrics := &countingAuthMetrics{}
	verifier := newVerifier(t, server.URL, time.Now, withJWKSMetrics(metrics))
	known := f.validToken(t)
	if _, err := verifier.Verify(t.Context(), known); err != nil {
		t.Fatalf("Verify with the key set fetched at startup: %v", err)
	}

	serveOversized.Store(true)
	f.privateKey, f.keyID = keyB, "key-b"
	_, err := verifier.Verify(t.Context(), f.validToken(t))
	assertVerifierUnavailable(t, err)
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Errorf("JWKSFetchFailed after an oversized JWKS body: got %d, want 1", got)
	}

	if _, err := verifier.Verify(t.Context(), known); err != nil {
		t.Fatalf("an oversized JWKS body evicted the cached key set: %v", err)
	}
}

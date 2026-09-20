package providers

import (
	"altune/go-api/internal/auth"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
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

func TestCappedJWKSBody_ReadsUpToTheCapAndFailsPastIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		want error
	}{
		{"one byte under the cap", maxJWKSBodyBytes - 1, nil},
		{"exactly at the cap", maxJWKSBodyBytes, nil},
		{"one byte over the cap", maxJWKSBodyBytes + 1, errJWKSBodyTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &cappedJWKSBody{
				body:      io.NopCloser(bytes.NewReader(make([]byte, tc.size))),
				remaining: maxJWKSBodyBytes,
			}

			read, err := io.ReadAll(body)

			if !errors.Is(err, tc.want) {
				t.Fatalf("reading a %d-byte body: got %v, want %v", tc.size, err, tc.want)
			}
			if tc.want == nil && len(read) != tc.size {
				t.Errorf("bytes delivered: got %d, want %d", len(read), tc.size)
			}
		})
	}
}

// A compressed response is the cheap way to blow past the cap: a few KB on the
// wire expand to gigabytes. The transport wraps the body the HTTP client hands
// back, which is already the decompressing reader, so the cap counts the bytes
// that reach memory rather than the bytes on the wire.
func TestCappedJWKSBodyTransport_CapsDecompressedBytes(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(make([]byte, maxJWKSBodyBytes+1)); err != nil {
		t.Fatalf("compress body: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close compressed body: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	t.Cleanup(server.Close)
	client := &http.Client{Transport: cappedJWKSBodyTransport{base: http.DefaultTransport}}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)

	if !errors.Is(err, errJWKSBodyTooLarge) {
		t.Fatalf("reading a %d-byte body compressed to %d bytes: got %v, want %v",
			maxJWKSBodyBytes+1, compressed.Len(), err, errJWKSBodyTooLarge)
	}
}

// marshalKeySet renders the JWKS JSON an endpoint publishes for one public key.
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

// A JWKS endpoint, or anything in front of it, must not be able to make the
// verifier buffer an unbounded response: the fetch reads the body with
// io.ReadAll, and an unauthenticated caller can force one with unknown-kid
// tokens (#2181).
func TestSupabaseJWTVerifier_OversizedJWKSBodyKeepsCachedKeys(t *testing.T) {
	keyA, keyB := generateRSAKey(t), generateRSAKey(t)
	// Padding whitespace JSON ignores: only a client that finishes reading the
	// body sees a valid key set, so the cap is what this test isolates.
	oversized := append(marshalKeySet(t, &keyB.PublicKey, "key-b"), bytes.Repeat([]byte{' '}, maxJWKSBodyBytes)...)
	withinCap := marshalKeySet(t, &keyA.PublicKey, "key-a")

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

	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix
	metrics := &countingAuthMetrics{}
	verifier, err := NewSupabaseJWTVerifier(t.Context(), server.URL, f.projectURL, f.audience, WithJWKSMetrics(metrics))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	// A normal key set still loads: the startup fetch primed key A.
	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("Verify with the key set fetched at startup: %v", err)
	}

	// The endpoint turns oversized, and an unknown kid forces a refresh onto it.
	serveOversized.Store(true)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err = verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience)))
	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Errorf("JWKSFetchFailed after an oversized JWKS body: got %d, want 1", got)
	}

	// Uncapped, that body would have parsed and replaced the cached set, so
	// every token signed under key A would start failing.
	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("an oversized JWKS body evicted the cached key set: %v", err)
	}
}

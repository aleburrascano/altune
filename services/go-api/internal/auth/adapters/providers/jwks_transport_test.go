package providers

import (
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

func TestSupabaseJWTVerifier_OversizedJWKSBodyKeepsCachedKeys(t *testing.T) {
	keyA, keyB := generateRSAKey(t), generateRSAKey(t)
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

	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("Verify with the key set fetched at startup: %v", err)
	}

	serveOversized.Store(true)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err = verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience)))
	assertVerifierUnavailable(t, err)
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Errorf("JWKSFetchFailed after an oversized JWKS body: got %d, want 1", got)
	}

	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("an oversized JWKS body evicted the cached key set: %v", err)
	}
}

package providers

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/ports"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type testJWTFixture struct {
	privateKey *rsa.PrivateKey
	jwksServer *httptest.Server
	projectURL string
	audience   string
	issuer     string
	keyID      string
}

func newTestJWTFixture(t *testing.T) *testJWTFixture {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	keyID := "test-key-1"
	pubJWK := signingJWK(t, privKey.PublicKey, keyID)

	keySet := jwk.NewSet()
	_ = keySet.AddKey(pubJWK)

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keySet)
	}))
	t.Cleanup(jwksServer.Close)

	projectURL := "https://test-project.supabase.co"
	audience := "authenticated"

	return &testJWTFixture{
		privateKey: privKey,
		jwksServer: jwksServer,
		projectURL: projectURL,
		audience:   audience,
		issuer:     projectURL + supabaseAuthPathSuffix,
		keyID:      keyID,
	}
}

func (f *testJWTFixture) signToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()

	builder := jwt.New()
	for k, v := range claims {
		if err := builder.Set(k, v); err != nil {
			t.Fatalf("set claim %q: %v", k, err)
		}
	}

	privJWK := signingJWK(t, f.privateKey, f.keyID)

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, privJWK))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

func (f *testJWTFixture) newVerifier(t *testing.T) *SupabaseJWTVerifier {
	t.Helper()
	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, f.jwksServer.URL, f.projectURL, f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	return verifier
}

func signingJWK(t *testing.T, raw any, kid string) jwk.Key {
	t.Helper()
	key, err := jwk.FromRaw(raw)
	if err != nil {
		t.Fatalf("create JWK: %v", err)
	}
	_ = key.Set(jwk.KeyIDKey, kid)
	_ = key.Set(jwk.AlgorithmKey, jwa.RS256)
	_ = key.Set(jwk.KeyUsageKey, "sig")
	return key
}

func assertInvalidTokenReason(t *testing.T, err error, want auth.TokenRejectReason) {
	t.Helper()
	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if tokenErr.Reason != want {
		t.Errorf("reason: got %q, want %q", tokenErr.Reason, want)
	}
}

func TestSupabaseJWTVerifier_ValidToken(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	sub := uuid.New().String()
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(30 * time.Minute),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	verified, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
}

func TestSupabaseJWTVerifier_IssuerGoldenValue(t *testing.T) {
	f := newTestJWTFixture(t)
	const want = "https://test-project.supabase.co/auth/v1"
	for _, projectURL := range []string{f.projectURL, f.projectURL + "/", f.projectURL + "//"} {
		verifier, err := NewSupabaseJWTVerifier(context.Background(), f.jwksServer.URL, projectURL, f.audience)
		if err != nil {
			t.Fatalf("create verifier for %q: %v", projectURL, err)
		}
		if verifier.issuer != want {
			t.Errorf("issuer for %q: got %q, want %q", projectURL, verifier.issuer, want)
		}
	}
}

func TestSupabaseJWTVerifier_ProjectURLTrailingSlash(t *testing.T) {
	f := newTestJWTFixture(t)

	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, f.jwksServer.URL, f.projectURL+"/", f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	sub := uuid.New().String()
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(30 * time.Minute),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	verified, err := verifier.Verify(ctx, token)
	if err != nil {
		t.Fatalf("Verify with trailing-slash project URL: %v", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
}

func TestSupabaseJWTVerifier_ExpiredToken(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(-1 * time.Hour),
		"iat": time.Now().Add(-2 * time.Hour),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonExpired)
}

func TestSupabaseJWTVerifier_InvalidSignature(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	wrongJWK := signingJWK(t, wrongKey, f.keyID)

	builder := jwt.New()
	_ = builder.Set("sub", uuid.New().String())
	_ = builder.Set("iss", f.issuer)
	_ = builder.Set("aud", f.audience)
	_ = builder.Set("exp", time.Now().Add(1*time.Hour))

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, wrongJWK))
	if err != nil {
		t.Fatalf("sign with wrong key: %v", err)
	}

	_, err = verifier.Verify(context.Background(), string(signed))
	if err == nil {
		t.Fatal("expected error for wrong-key signature, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
}

func TestSupabaseJWTVerifier_UnknownKeyID(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	strayKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate stray key: %v", err)
	}
	strayJWK := signingJWK(t, strayKey, "not-in-jwks")

	builder := jwt.New()
	_ = builder.Set("sub", uuid.New().String())
	_ = builder.Set("iss", f.issuer)
	_ = builder.Set("aud", f.audience)
	_ = builder.Set("exp", time.Now().Add(1*time.Hour))

	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, strayJWK))
	if err != nil {
		t.Fatalf("sign with stray key: %v", err)
	}

	_, err = verifier.Verify(context.Background(), string(signed))
	if err == nil {
		t.Fatal("expected error for unknown key id, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
}

func TestSupabaseJWTVerifier_JWKSUnavailable(t *testing.T) {
	ctx := context.Background()
	const unreachableJWKSURL = "http://127.0.0.1:1/jwks"
	verifier, err := NewSupabaseJWTVerifier(ctx, unreachableJWKSURL, "https://test-project.supabase.co", "authenticated")
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	_, err = verifier.Verify(ctx, "any-token")
	if err == nil {
		t.Fatal("expected error when JWKS is unreachable, got nil")
	}

	var tokenErr *auth.InvalidTokenError
	if errors.As(err, &tokenErr) {
		t.Fatalf("JWKS unavailability must not be an InvalidTokenError, got reason %q", tokenErr.Reason)
	}
}

func TestSupabaseJWTVerifier_MissingExp(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"iat": time.Now().Add(-1 * time.Minute),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for token missing exp claim, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonClaimMissingEXP)
}

func TestSupabaseJWTVerifier_SlowJWKSEndpointDoesNotHang(t *testing.T) {
	blockForever := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-blockForever
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(blockForever) })

	origTimeout := jwksFetchTimeout
	jwksFetchTimeout = 200 * time.Millisecond
	t.Cleanup(func() { jwksFetchTimeout = origTimeout })

	const bound = 5 * time.Second

	startupDone := make(chan *SupabaseJWTVerifier, 1)
	go func() {
		v, err := NewSupabaseJWTVerifier(context.Background(), server.URL, "https://test-project.supabase.co", "authenticated")
		if err != nil {
			t.Errorf("NewSupabaseJWTVerifier returned error: %v", err)
		}
		startupDone <- v
	}()

	var verifier *SupabaseJWTVerifier
	select {
	case verifier = <-startupDone:
	case <-time.After(bound):
		t.Fatalf("NewSupabaseJWTVerifier hung on a slow JWKS endpoint (no return within %s)", bound)
	}
	if verifier == nil {
		t.Fatal("verifier was nil")
	}

	verifyDone := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), "any-token")
		verifyDone <- err
	}()

	select {
	case err := <-verifyDone:
		if err == nil {
			t.Fatal("expected an error from a hung JWKS endpoint, got nil")
		}
	case <-time.After(bound):
		t.Fatalf("Verify hung on a slow JWKS endpoint (no return within %s)", bound)
	}
}

func newTogglingJWKSFixture(t *testing.T) (*testJWTFixture, string, *atomic.Bool) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	keyID := "test-key-1"
	keySet := jwk.NewSet()
	_ = keySet.AddKey(signingJWK(t, privKey.PublicKey, keyID))

	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(keySet)
	}))
	t.Cleanup(server.Close)

	projectURL := "https://test-project.supabase.co"
	f := &testJWTFixture{
		privateKey: privKey,
		jwksServer: server,
		projectURL: projectURL,
		audience:   "authenticated",
		issuer:     projectURL + supabaseAuthPathSuffix,
		keyID:      keyID,
	}
	return f, server.URL, &healthy
}

func TestSupabaseJWTVerifier_TransientStartupFailureRecoversOnNextRequest(t *testing.T) {
	f, jwksURL, healthy := newTogglingJWKSFixture(t)

	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, jwksURL, f.projectURL, f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	healthy.Store(true)

	sub := uuid.New().String()
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(30 * time.Minute),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	verified, err := verifier.Verify(ctx, token)
	if err != nil {
		t.Fatalf("Verify after endpoint recovery: %v (retry on next request did not happen)", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
}

func TestSupabaseJWTVerifier_CheckHealth(t *testing.T) {
	f, jwksURL, healthy := newTogglingJWKSFixture(t)

	ctx := context.Background()
	verifier, err := NewSupabaseJWTVerifier(ctx, jwksURL, f.projectURL, f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	if err := verifier.CheckHealth(ctx); err == nil {
		t.Fatal("expected CheckHealth to report degraded auth while JWKS is down")
	}

	healthy.Store(true)
	if err := verifier.CheckHealth(ctx); !errors.Is(err, errJWKSRefreshBackoff) {
		t.Fatalf("expected a backoff error inside the window, got: %v", err)
	}

	clock := time.Now().Add(jwksRefreshBackoffCap)
	verifier.refresher.now = func() time.Time { return clock }
	if err := verifier.CheckHealth(ctx); err != nil {
		t.Fatalf("expected healthy auth after endpoint recovery, got: %v", err)
	}
}

func shortenJWKSBackgroundRefresh(t *testing.T) {
	t.Helper()
	origInterval, origWindow := jwksBackgroundRefreshInterval, jwksRefreshWindow
	jwksBackgroundRefreshInterval, jwksRefreshWindow = time.Second, time.Second
	t.Cleanup(func() { jwksBackgroundRefreshInterval, jwksRefreshWindow = origInterval, origWindow })
}

func waitFor(t *testing.T, bound time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", bound, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSupabaseJWTVerifier_CheckHealthDegradesAfterSustainedBackgroundRefreshFailure(t *testing.T) {
	shortenJWKSBackgroundRefresh(t)
	keyA := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated", privateKey: keyA, keyID: "key-a"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix

	var skew atomic.Int64
	clock := func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }
	ctx := t.Context()
	verifier, err := newSupabaseJWTVerifier(ctx, jwks.server.URL, f.projectURL, f.audience, clock)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if err := verifier.CheckHealth(ctx); err != nil {
		t.Fatalf("CheckHealth right after a successful startup fetch: %v", err)
	}

	jwks.down.Store(true)
	waitFor(t, 10*time.Second, "two failed background refreshes to reach the error sink", func() bool {
		verifier.refresher.mu.Lock()
		defer verifier.refresher.mu.Unlock()
		return verifier.refresher.bgFailures >= 2
	})

	if _, err := verifier.Verify(ctx, f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("stale-but-usable key set stopped verifying: %v", err)
	}
	skew.Store(int64(jwksStaleAfter - time.Minute))
	if err := verifier.CheckHealth(ctx); err != nil {
		t.Fatalf("CheckHealth inside the staleness bound: got %v, want healthy", err)
	}

	skew.Store(int64(jwksStaleAfter + time.Minute))
	err = verifier.CheckHealth(ctx)
	if err == nil || !errors.Is(err, errJWKSStale) {
		t.Fatalf("CheckHealth after sustained background refresh failure: got %v, want errJWKSStale", err)
	}
	if !strings.Contains(err.Error(), "consecutive background refresh failures") {
		t.Errorf("stale health error lacks the failure streak: %v", err)
	}

	jwks.down.Store(false)
	waitFor(t, 10*time.Second, "a background refresh to clear staleness", func() bool {
		return verifier.CheckHealth(ctx) == nil
	})
}

func TestSupabaseJWTVerifier_EmptyKeySetDoesNotResetStaleness(t *testing.T) {
	var skew atomic.Int64
	v := &SupabaseJWTVerifier{}
	v.refresher = newJWKSRefresher(v.forceRefresh)
	v.refresher.now = func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }

	key := signingJWK(t, generateRSAKey(t).PublicKey, "key-a")
	full := jwk.NewSet()
	_ = full.AddKey(key)
	if _, err := v.onKeySetFetched("", full); err != nil {
		t.Fatalf("post-fetch hook: %v", err)
	}

	skew.Store(int64(jwksStaleAfter + time.Minute))
	if _, err := v.onKeySetFetched("", jwk.NewSet()); !errors.Is(err, errJWKSEmptyKeySet) {
		t.Fatalf("post-fetch hook on an empty key set: got %v, want errJWKSEmptyKeySet", err)
	}
	if err := v.refresher.checkFresh(); !errors.Is(err, errJWKSStale) {
		t.Fatalf("an empty key set refreshed staleness: got %v, want errJWKSStale", err)
	}

	if _, err := v.onKeySetFetched("", full); err != nil {
		t.Fatalf("post-fetch hook: %v", err)
	}
	if err := v.refresher.checkFresh(); err != nil {
		t.Fatalf("a non-empty key set did not reset staleness: %v", err)
	}
}

func newCountingJWKSServer(t *testing.T, delay time.Duration, release <-chan struct{}) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		select {
		case <-time.After(delay):
		case <-release:
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestSupabaseJWTVerifier_UnprimedConcurrentVerifyCoalescesFetches(t *testing.T) {
	server, hits := newCountingJWKSServer(t, 200*time.Millisecond, nil)
	verifier, err := NewSupabaseJWTVerifier(context.Background(), server.URL, "https://test-project.supabase.co", "authenticated")
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	hits.Store(0)

	const callers = 20
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := verifier.Verify(context.Background(), "any-token")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err == nil {
			t.Fatal("expected every caller to fail while JWKS is down")
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches for %d concurrent unprimed callers: got %d, want 1 (coalesced)", callers, got)
	}
}

func TestSupabaseJWTVerifier_UnprimedFailureBacksOffInsteadOfRetryingEveryRequest(t *testing.T) {
	server, hits := newCountingJWKSServer(t, 0, nil)
	verifier, err := NewSupabaseJWTVerifier(context.Background(), server.URL, "https://test-project.supabase.co", "authenticated")
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	hits.Store(0)

	for range 10 {
		if _, err := verifier.Verify(context.Background(), "any-token"); err == nil {
			t.Fatal("expected an error while JWKS is down")
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches for 10 sequential failing requests: got %d, want 1 (backed off)", got)
	}
}

func TestSupabaseJWTVerifier_CanceledCallerIsReleasedFromSharedFetch(t *testing.T) {
	release := make(chan struct{})
	server, _ := newCountingJWKSServer(t, time.Minute, release)
	t.Cleanup(func() { close(release) })

	origTimeout := jwksFetchTimeout
	jwksFetchTimeout = 2 * time.Second
	t.Cleanup(func() { jwksFetchTimeout = origTimeout })

	verifier, err := NewSupabaseJWTVerifier(context.Background(), server.URL, "https://test-project.supabase.co", "authenticated")
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	go func() { _, _ = verifier.Verify(context.Background(), "any-token") }()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = verifier.Verify(ctx, "any-token")
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("canceled caller stayed parked on the shared fetch for %s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller's own deadline error, got %v", err)
	}
}

type rotatingJWKSServer struct {
	server *httptest.Server
	keySet atomic.Pointer[jwk.Set]
	hits   atomic.Int64
	down   atomic.Bool
}

func newRotatingJWKSServer(t *testing.T, pub *rsa.PublicKey, kid string) *rotatingJWKSServer {
	t.Helper()
	s := &rotatingJWKSServer{}
	s.rotate(t, pub, kid)
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		if s.down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(*s.keySet.Load())
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *rotatingJWKSServer) rotate(t *testing.T, pub *rsa.PublicKey, kid string) {
	t.Helper()
	set := jwk.NewSet()
	_ = set.AddKey(signingJWK(t, *pub, kid))
	s.keySet.Store(&set)
}

func (s *rotatingJWKSServer) publishNoKeys() {
	empty := jwk.NewSet()
	s.keySet.Store(&empty)
}

func validClaims(issuer, audience string) map[string]interface{} {
	return map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": issuer,
		"aud": audience,
		"exp": time.Now().Add(30 * time.Minute),
		"iat": time.Now().Add(-1 * time.Minute),
	}
}

func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func TestSupabaseJWTVerifier_KeyRotationRefreshesOnceAndAcceptsNewKey(t *testing.T) {
	keyA, keyB := generateRSAKey(t), generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix
	f.jwksServer = jwks.server
	verifier := f.newVerifier(t)

	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("Verify with pre-rotation key: %v", err)
	}

	jwks.rotate(t, &keyB.PublicKey, "key-b")
	jwks.hits.Store(0)
	f.privateKey, f.keyID = keyB, "key-b"

	if _, err := verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("Verify with rotated key was rejected (no refresh-and-retry): %v", err)
	}
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches after rotation: got %d, want exactly 1", got)
	}
}

func TestSupabaseJWTVerifier_UnknownKidFloodCannotForceUnboundedFetches(t *testing.T) {
	keyA := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix
	f.jwksServer = jwks.server
	verifier := f.newVerifier(t)
	jwks.hits.Store(0)

	f.privateKey = generateRSAKey(t)
	const sequential, concurrent = 50, 50
	for range sequential {
		f.keyID = uuid.New().String()
		_, err := verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience)))
		assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
	}

	tokens := make([]string, concurrent)
	for i := range tokens {
		f.keyID = uuid.New().String()
		tokens[i] = f.signToken(t, validClaims(f.issuer, f.audience))
	}
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := verifier.Verify(context.Background(), token)
			assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
		}()
	}
	wg.Wait()

	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches for %d unknown-kid tokens: got %d, want 1 (rate-limited)", sequential+concurrent, got)
	}

	clock := time.Now().Add(jwksUnknownKeyRefreshInterval)
	verifier.refresher.now = func() time.Time { return clock }
	f.keyID = uuid.New().String()
	_, _ = verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience)))
	if got := jwks.hits.Load(); got != 2 {
		t.Fatalf("JWKS fetches after the refresh interval: got %d, want 2", got)
	}
}

func TestSupabaseJWTVerifier_FailedUnknownKidRefreshKeepsCachedKeys(t *testing.T) {
	keyA := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix
	f.jwksServer = jwks.server
	verifier := f.newVerifier(t)

	jwks.down.Store(true)
	jwks.hits.Store(0)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err := verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience)))
	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches: got %d, want 1 failed forced refresh", got)
	}

	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(context.Background(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("a failed forced refresh evicted the cached key set: %v", err)
	}
}

func TestSupabaseJWTVerifier_RefreshPublishingNoKeysKeepsCachedKeys(t *testing.T) {
	keyA := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix
	metrics := &countingAuthMetrics{}
	verifier, err := NewSupabaseJWTVerifier(t.Context(), jwks.server.URL, f.projectURL, f.audience, WithJWKSMetrics(metrics))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	jwks.publishNoKeys()
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err = verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience)))
	assertInvalidTokenReason(t, err, auth.ReasonSignatureInvalid)

	f.privateKey, f.keyID = keyA, "key-a"
	if _, err := verifier.Verify(t.Context(), f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("a JWKS response with no keys evicted the cached key set: %v", err)
	}
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Errorf("JWKSFetchFailed after a refresh that published no keys: got %d, want 1", got)
	}
}

func TestSupabaseJWTVerifier_ColdStartWithNoKeysIsUnhealthyUntilKeysArrive(t *testing.T) {
	keyA := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &keyA.PublicKey, "key-a")
	jwks.publishNoKeys()
	f := &testJWTFixture{projectURL: "https://test-project.supabase.co", audience: "authenticated", privateKey: keyA, keyID: "key-a"}
	f.issuer = f.projectURL + supabaseAuthPathSuffix

	ctx := t.Context()
	verifier, err := NewSupabaseJWTVerifier(ctx, jwks.server.URL, f.projectURL, f.audience)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	if err := verifier.CheckHealth(ctx); !errors.Is(err, errJWKSEmptyKeySet) {
		t.Fatalf("CheckHealth after a cold start that fetched no keys: got %v, want errJWKSEmptyKeySet", err)
	}

	jwks.rotate(t, &keyA.PublicKey, "key-a")
	clock := time.Now().Add(jwksRefreshBackoffCap)
	verifier.refresher.now = func() time.Time { return clock }
	if err := verifier.CheckHealth(ctx); err != nil {
		t.Fatalf("CheckHealth once the endpoint publishes keys: %v", err)
	}
	if _, err := verifier.Verify(ctx, f.signToken(t, validClaims(f.issuer, f.audience))); err != nil {
		t.Fatalf("Verify once the endpoint publishes keys: %v", err)
	}
}

func TestSupabaseJWTVerifier_MissingSub(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(30 * time.Minute),
		"iat": time.Now().Add(-1 * time.Minute),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for missing sub claim, got nil")
	}

	assertInvalidTokenReason(t, err, auth.ReasonClaimInvalidSUB)
}

func TestSupabaseJWTVerifier_LifetimeAtMaximumAccepted(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	iat := time.Now().Add(-1 * time.Minute)
	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"iat": iat,
		"exp": iat.Add(maxAccessTokenLifetime),
	})

	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify token with exactly the maximum lifetime: %v", err)
	}
}

func TestSupabaseJWTVerifier_LifetimeOverMaximumRejected(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	iat := time.Now().Add(-1 * time.Minute)
	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"iat": iat,
		"exp": iat.Add(24 * time.Hour),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for token lifetime over maximum, got nil")
	}
	assertInvalidTokenReason(t, err, auth.ReasonClaimInvalidIAT)
}

func TestSupabaseJWTVerifier_MissingIatRejected(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"exp": time.Now().Add(30 * time.Minute),
	})

	_, err := verifier.Verify(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for token missing iat claim, got nil")
	}
	assertInvalidTokenReason(t, err, auth.ReasonClaimInvalidIAT)
}

func TestSupabaseJWTVerifier_FutureIatCannotShrinkLifetime(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	iat := time.Now().Add(30 * time.Minute)
	token := f.signToken(t, map[string]interface{}{
		"sub": uuid.New().String(),
		"iss": f.issuer,
		"aud": f.audience,
		"iat": iat,
		"exp": iat.Add(maxAccessTokenLifetime),
	})

	if _, err := verifier.Verify(context.Background(), token); err == nil {
		t.Fatal("expected error for forward-dated iat, got nil")
	}
}

func TestSupabaseJWTVerifier_VerifyReportsTokenExpiry(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	sub := uuid.New().String()
	exp := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	token := f.signToken(t, map[string]interface{}{
		"sub": sub,
		"iss": f.issuer,
		"aud": f.audience,
		"exp": exp,
		"iat": time.Now().Add(-1 * time.Minute),
	})

	verified, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID.String() != sub {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), sub)
	}
	if !verified.ExpiresAt.Equal(exp) {
		t.Errorf("expiry: got %v, want the token's exp %v", verified.ExpiresAt, exp)
	}
}

func TestVerifiedToken_RejectsTokenWithoutExpiry(t *testing.T) {
	token, err := jwt.NewBuilder().Subject(uuid.New().String()).Build()
	if err != nil {
		t.Fatalf("build token: %v", err)
	}

	_, err = verifiedToken(token)

	var invalid *auth.InvalidTokenError
	if !errors.As(err, &invalid) || invalid.Reason != auth.ReasonClaimMissingEXP {
		t.Fatalf("err = %v, want InvalidTokenError with reason %q", err, auth.ReasonClaimMissingEXP)
	}
}

type countingAuthMetrics struct{ jwksFailures atomic.Int64 }

var _ ports.AuthMetrics = (*countingAuthMetrics)(nil)

func (*countingAuthMetrics) TokenRejected(string) {}
func (*countingAuthMetrics) RequestThrottled()    {}
func (*countingAuthMetrics) VerifierUnavailable() {}
func (m *countingAuthMetrics) JWKSFetchFailed()   { m.jwksFailures.Add(1) }

func TestSupabaseJWTVerifier_CountsStartupAndForcedJWKSFetchFailures(t *testing.T) {
	server, hits := newCountingJWKSServer(t, 0, nil)
	metrics := &countingAuthMetrics{}
	verifier, err := NewSupabaseJWTVerifier(context.Background(), server.URL,
		"https://test-project.supabase.co", "authenticated", WithJWKSMetrics(metrics))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Fatalf("after failed startup fetch: JWKSFetchFailed %d, want 1", got)
	}

	for range 5 {
		if _, err := verifier.Verify(context.Background(), "any-token"); err == nil {
			t.Fatal("expected an error while JWKS is down")
		}
	}
	if got, fetches := metrics.jwksFailures.Load(), hits.Load(); got != 2 || fetches != 2 {
		t.Fatalf("after 5 requests during the outage: JWKSFetchFailed %d over %d real fetches, want 2 and 2", got, fetches)
	}
}

func TestSupabaseJWTVerifier_CountsBackgroundJWKSRefreshFailures(t *testing.T) {
	shortenJWKSBackgroundRefresh(t)
	key := generateRSAKey(t)
	jwks := newRotatingJWKSServer(t, &key.PublicKey, "key-a")
	metrics := &countingAuthMetrics{}
	verifier, err := NewSupabaseJWTVerifier(t.Context(), jwks.server.URL,
		"https://test-project.supabase.co", "authenticated", WithJWKSMetrics(metrics))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if got := metrics.jwksFailures.Load(); got != 0 {
		t.Fatalf("after successful startup fetch: JWKSFetchFailed %d, want 0", got)
	}

	jwks.down.Store(true)
	waitFor(t, 10*time.Second, "failed background refreshes to be counted", func() bool {
		verifier.refresher.mu.Lock()
		defer verifier.refresher.mu.Unlock()
		return verifier.refresher.bgFailures >= 2 && metrics.jwksFailures.Load() >= 2
	})
}

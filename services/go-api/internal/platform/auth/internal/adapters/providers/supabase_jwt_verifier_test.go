package providers_test

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/auth/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"altune/go-api/internal/shared/random"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"log/slog"
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

const (
	testProjectURL = "https://test-project.supabase.co"
	testIssuer     = testProjectURL + "/auth/v1"
	testAudience   = "authenticated"
	staleAfter     = time.Hour
	backoffCap     = 30 * time.Second
	unknownKidGap  = 30 * time.Second
)

var fastBackgroundRefresh = withJWKSBackgroundRefresh(time.Second, time.Second)

type silentAuthMetrics struct{}

func (silentAuthMetrics) TokenRejected(string) {}
func (silentAuthMetrics) RequestThrottled()    {}
func (silentAuthMetrics) VerifierUnavailable() {}
func (silentAuthMetrics) JWKSFetchFailed()     {}

type testClock struct{ offset atomic.Int64 }

func (c *testClock) now() time.Time {
	return time.Unix(1_700_000_000, 0).Add(time.Duration(c.offset.Load()))
}

func (c *testClock) advance(d time.Duration) { c.offset.Add(int64(d)) }

type testJWTFixture struct {
	privateKey *rsa.PrivateKey
	jwksServer *httptest.Server
	projectURL string
	audience   string
	issuer     string
	keyID      string
}

func newKeylessFixture() *testJWTFixture {
	return &testJWTFixture{projectURL: testProjectURL, audience: testAudience, issuer: testIssuer}
}

func newTestJWTFixture(t *testing.T) *testJWTFixture {
	t.Helper()
	f := newKeylessFixture()
	f.privateKey, f.keyID = generateRSAKey(t), "test-key-1"
	keySet := jwk.NewSet()
	_ = keySet.AddKey(signingJWK(t, f.privateKey.PublicKey, f.keyID))
	keySetJSON, err := json.Marshal(keySet)
	if err != nil {
		t.Fatalf("encode the key set: %v", err)
	}
	f.jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(keySetJSON)
	}))
	t.Cleanup(f.jwksServer.Close)
	return f
}

func (f *testJWTFixture) signToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	builder := jwt.New()
	for k, v := range claims {
		if err := builder.Set(k, v); err != nil {
			t.Fatalf("set claim %q: %v", k, err)
		}
	}
	signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, signingJWK(t, f.privateKey, f.keyID)))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return string(signed)
}

func (f *testJWTFixture) validToken(t *testing.T) string {
	t.Helper()
	return f.signToken(t, validClaims(f.issuer, f.audience))
}

func (f *testJWTFixture) newVerifier(t *testing.T) *auth.SupabaseJWTVerifier {
	t.Helper()
	return newVerifier(t, f.jwksServer.URL, time.Now)
}

func newVerifier(t *testing.T, jwksURL string, now func() time.Time, opts ...verifierOption) *auth.SupabaseJWTVerifier {
	t.Helper()
	settings := newVerifierSettings(opts)
	verifier, err := auth.NewSupabaseJWTVerifier(t.Context(), jwksURL, testProjectURL, testAudience, now, random.Real, settings.metrics, settings.revoker, settings.schedule)
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

func assertInvalidTokenReason(t *testing.T, err error, want string) {
	t.Helper()
	var tokenErr *auth.InvalidTokenError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("expected InvalidTokenError, got %T: %v", err, err)
	}
	if string(tokenErr.Reason) != want {
		t.Errorf("reason: got %q, want %q", tokenErr.Reason, want)
	}
}

func assertVerifierUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a verifier-unavailable error, got nil")
	}
	var tokenErr *auth.InvalidTokenError
	if errors.As(err, &tokenErr) {
		t.Fatalf("a failed key refresh was reported as a token rejection (%q): %v", tokenErr.Reason, err)
	}
}

func assertErrorSays(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error %v, want one saying %q", err, want)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error %v, want %v", err, target)
	}
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

func waitFor(t *testing.T, bound time.Duration, what string, cond func() bool) {
	t.Helper()
	polltest.Until(t, bound, what, cond)
}

type rotatingJWKSServer struct {
	server  *httptest.Server
	keySet  atomic.Pointer[jwk.Set]
	hits    atomic.Int64
	down    atomic.Bool
	stall   atomic.Bool
	release chan struct{}
}

func newRotatingJWKSServer(t *testing.T, pub *rsa.PublicKey, kid string) *rotatingJWKSServer {
	t.Helper()
	s := &rotatingJWKSServer{release: make(chan struct{})}
	s.rotate(t, pub, kid)
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	t.Cleanup(func() { close(s.release) })
	return s
}

func (s *rotatingJWKSServer) serve(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	if s.stall.Load() {
		select {
		case <-r.Context().Done():
		case <-s.release:
		}
		return
	}
	if s.down.Load() {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(*s.keySet.Load())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
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

func newRotatingFixture(t *testing.T) (*testJWTFixture, *rotatingJWKSServer) {
	t.Helper()
	f := newKeylessFixture()
	f.privateKey, f.keyID = generateRSAKey(t), "key-a"
	jwks := newRotatingJWKSServer(t, &f.privateKey.PublicKey, f.keyID)
	f.jwksServer = jwks.server
	return f, jwks
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

type countingAuthMetrics struct{ jwksFailures atomic.Int64 }

func (*countingAuthMetrics) TokenRejected(string) {}
func (*countingAuthMetrics) RequestThrottled()    {}
func (*countingAuthMetrics) VerifierUnavailable() {}
func (m *countingAuthMetrics) JWKSFetchFailed()   { m.jwksFailures.Add(1) }

func TestSupabaseJWTVerifier_ValidToken(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	claims := validClaims(f.issuer, f.audience)

	verified, err := verifier.Verify(t.Context(), f.signToken(t, claims))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID.String() != claims["sub"] {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), claims["sub"])
	}
}

func TestSupabaseJWTVerifier_IssuerGoldenValue(t *testing.T) {
	f := newTestJWTFixture(t)
	token := f.signToken(t, validClaims("https://test-project.supabase.co/auth/v1", f.audience))
	for _, projectURL := range []string{f.projectURL, f.projectURL + "/", f.projectURL + "//"} {
		verifier, err := auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, projectURL, f.audience, time.Now, random.Real, silentAuthMetrics{}, auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule())
		if err != nil {
			t.Fatalf("create verifier for %q: %v", projectURL, err)
		}
		if _, err := verifier.Verify(t.Context(), token); err != nil {
			t.Errorf("project URL %q rejected a token issued by %s/auth/v1: %v", projectURL, testProjectURL, err)
		}
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

	_, err := verifier.Verify(t.Context(), token)

	assertInvalidTokenReason(t, err, "expired")
}

func TestSupabaseJWTVerifier_JWKSUnavailable(t *testing.T) {
	verifier := newVerifier(t, "http://127.0.0.1:1/jwks", time.Now)

	_, err := verifier.Verify(t.Context(), "any-token")

	assertVerifierUnavailable(t, err)
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

	_, err := verifier.Verify(t.Context(), token)

	assertInvalidTokenReason(t, err, "claim_missing_exp")
}

func TestSupabaseJWTVerifier_SlowJWKSEndpointDoesNotHang(t *testing.T) {
	blockForever := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-blockForever }))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(blockForever) })
	const bound = 5 * time.Second

	startupDone := make(chan *auth.SupabaseJWTVerifier, 1)
	go func() {
		v, err := auth.NewSupabaseJWTVerifier(context.Background(), server.URL, testProjectURL, testAudience, time.Now, random.Real, silentAuthMetrics{}, auth.NoopTokenRevoker(), auth.JWKSSchedule{FetchTimeout: 200 * time.Millisecond})
		if err != nil {
			t.Errorf("NewSupabaseJWTVerifier returned error: %v", err)
		}
		startupDone <- v
	}()
	var verifier *auth.SupabaseJWTVerifier
	select {
	case verifier = <-startupDone:
	case <-time.After(bound):
		t.Fatalf("NewSupabaseJWTVerifier hung on a slow JWKS endpoint (no return within %s)", bound)
	}

	verifyDone := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), "any-token")
		verifyDone <- err
	}()
	select {
	case err := <-verifyDone:
		assertVerifierUnavailable(t, err)
	case <-time.After(bound):
		t.Fatalf("Verify hung on a slow JWKS endpoint (no return within %s)", bound)
	}
}

func TestSupabaseJWTVerifier_TransientStartupFailureRecoversOnNextRequest(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	jwks.down.Store(true)
	verifier := f.newVerifier(t)

	jwks.down.Store(false)
	claims := validClaims(f.issuer, f.audience)
	verified, err := verifier.Verify(t.Context(), f.signToken(t, claims))
	if err != nil {
		t.Fatalf("Verify after endpoint recovery: %v (retry on next request did not happen)", err)
	}
	if verified.UserID.String() != claims["sub"] {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), claims["sub"])
	}
}

func TestSupabaseJWTVerifier_CheckHealth(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	jwks.down.Store(true)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now)

	if err := verifier.CheckHealth(t.Context()); err == nil {
		t.Fatal("expected CheckHealth to report degraded auth while JWKS is down")
	}

	jwks.down.Store(false)
	assertErrorIs(t, verifier.CheckHealth(t.Context()), auth.ErrJWKSRefreshBackoff)

	clock.advance(backoffCap)
	if err := verifier.CheckHealth(t.Context()); err != nil {
		t.Fatalf("expected healthy auth after endpoint recovery, got: %v", err)
	}
}

func TestSupabaseJWTVerifier_CheckHealthDegradesAfterSustainedBackgroundRefreshFailure(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	clock := &testClock{}
	metrics := &countingAuthMetrics{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now, withJWKSMetrics(metrics), fastBackgroundRefresh)
	if err := verifier.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth right after a successful startup fetch: %v", err)
	}

	jwks.down.Store(true)
	waitFor(t, 10*time.Second, "two failed background refreshes", func() bool { return metrics.jwksFailures.Load() >= 2 })
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("stale-but-usable key set stopped verifying: %v", err)
	}
	clock.advance(staleAfter)
	if err := verifier.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth at the staleness bound: got %v, want healthy", err)
	}

	clock.advance(time.Nanosecond)
	err := verifier.CheckHealth(t.Context())
	assertErrorIs(t, err, auth.ErrJWKSStale)
	assertErrorSays(t, err, "consecutive background refresh failures")

	jwks.down.Store(false)
	waitFor(t, 10*time.Second, "a background refresh to clear staleness", func() bool {
		return verifier.CheckHealth(t.Context()) == nil
	})
}

func TestSupabaseJWTVerifier_EmptyKeySetDoesNotResetStaleness(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now)

	jwks.publishNoKeys()
	clock.advance(staleAfter + time.Minute)
	_, err := verifier.Verify(t.Context(), f.validToken(t))
	assertErrorIs(t, err, auth.ErrJWKSStale)
	assertErrorIs(t, verifier.CheckHealth(t.Context()), auth.ErrJWKSStale)

	jwks.rotate(t, &f.privateKey.PublicKey, f.keyID)
	clock.advance(backoffCap)
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("a non-empty key set did not reset staleness: %v", err)
	}
	if err := verifier.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth after a non-empty refresh: %v", err)
	}
}

func TestSupabaseJWTVerifier_UnprimedConcurrentVerifyCoalescesFetches(t *testing.T) {
	server, hits := newCountingJWKSServer(t, 200*time.Millisecond, nil)
	verifier := newVerifier(t, server.URL, time.Now)
	hits.Store(0)

	const callers = 20
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			_, err := verifier.Verify(context.Background(), "any-token")
			errs <- err
		})
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
	verifier := newVerifier(t, server.URL, time.Now)
	hits.Store(0)

	for range 10 {
		if _, err := verifier.Verify(t.Context(), "any-token"); err == nil {
			t.Fatal("expected an error while JWKS is down")
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches for 10 sequential failing requests: got %d, want 1 (backed off)", got)
	}
}

func TestSupabaseJWTVerifier_CanceledCallerIsReleasedFromSharedFetch(t *testing.T) {
	release := make(chan struct{})
	server, hits := newCountingJWKSServer(t, time.Minute, release)
	t.Cleanup(func() { close(release) })
	verifier := newVerifier(t, server.URL, time.Now, withJWKSFetchTimeout(2*time.Second))

	go func() { _, _ = verifier.Verify(context.Background(), "any-token") }()
	waitFor(t, 10*time.Second, "the first fetch to reach the JWKS server", func() bool { return hits.Load() >= 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := verifier.Verify(ctx, "any-token")
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("canceled caller stayed parked on the shared fetch for %s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller's own deadline error, got %v", err)
	}
}

func TestSupabaseJWTVerifier_KeyRotationRefreshesOnceAndAcceptsNewKey(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	verifier := f.newVerifier(t)
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("Verify with pre-rotation key: %v", err)
	}

	f.privateKey, f.keyID = generateRSAKey(t), "key-b"
	jwks.rotate(t, &f.privateKey.PublicKey, f.keyID)
	jwks.hits.Store(0)

	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("Verify with rotated key was rejected (no refresh-and-retry): %v", err)
	}
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches after rotation: got %d, want exactly 1", got)
	}
}

func TestSupabaseJWTVerifier_UnknownKidFloodCannotForceUnboundedFetches(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now)
	jwks.hits.Store(0)
	f.privateKey = generateRSAKey(t)
	unknownKid := func() string {
		f.keyID = uuid.New().String()
		return f.validToken(t)
	}

	const sequential, concurrent = 50, 50
	for range sequential {
		_, err := verifier.Verify(t.Context(), unknownKid())
		assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))
	}
	tokens := make([]string, concurrent)
	for i := range tokens {
		tokens[i] = unknownKid()
	}
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Go(func() {
			_, err := verifier.Verify(context.Background(), token)
			assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))
		})
	}
	wg.Wait()
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches for %d unknown-kid tokens: got %d, want 1 (rate-limited)", sequential+concurrent, got)
	}

	clock.advance(unknownKidGap - time.Nanosecond)
	_, _ = verifier.Verify(t.Context(), unknownKid())
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches just inside the refresh interval: got %d, want 1", got)
	}
	clock.advance(time.Nanosecond)
	_, _ = verifier.Verify(t.Context(), unknownKid())
	if got := jwks.hits.Load(); got != 2 {
		t.Fatalf("JWKS fetches once the refresh interval elapsed: got %d, want 2", got)
	}
}

func TestSupabaseJWTVerifier_FailedUnknownKidRefreshKeepsCachedKeys(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	verifier := f.newVerifier(t)
	known := f.validToken(t)

	jwks.down.Store(true)
	jwks.hits.Store(0)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err := verifier.Verify(t.Context(), f.validToken(t))
	assertVerifierUnavailable(t, err)
	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches: got %d, want 1 failed forced refresh", got)
	}

	if _, err := verifier.Verify(t.Context(), known); err != nil {
		t.Fatalf("a failed forced refresh evicted the cached key set: %v", err)
	}
}

func TestSupabaseJWTVerifier_UnknownKidDuringRefreshBackoffIsUnavailable(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	verifier := f.newVerifier(t)

	jwks.down.Store(true)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	token := f.validToken(t)
	_, err := verifier.Verify(t.Context(), token)
	assertVerifierUnavailable(t, err)
	jwks.hits.Store(0)

	_, err = verifier.Verify(t.Context(), token)
	assertVerifierUnavailable(t, err)
	assertErrorIs(t, err, auth.ErrJWKSRefreshBackoff)
	if got := jwks.hits.Load(); got != 0 {
		t.Fatalf("JWKS fetches during backoff: got %d, want 0", got)
	}
}

func TestSupabaseJWTVerifier_UnknownKidInsideRecentRefreshWindowIsRejected(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	verifier := f.newVerifier(t)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	token := f.validToken(t)
	_, err := verifier.Verify(t.Context(), token)
	assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))

	jwks.down.Store(true)
	_, err = verifier.Verify(t.Context(), token)

	assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))
}

func TestSupabaseJWTVerifier_RefreshPublishingNoKeysKeepsCachedKeys(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	metrics := &countingAuthMetrics{}
	verifier := newVerifier(t, f.jwksServer.URL, time.Now, withJWKSMetrics(metrics))
	known := f.validToken(t)

	jwks.publishNoKeys()
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	_, err := verifier.Verify(t.Context(), f.validToken(t))
	assertVerifierUnavailable(t, err)

	if _, err := verifier.Verify(t.Context(), known); err != nil {
		t.Fatalf("a JWKS response with no keys evicted the cached key set: %v", err)
	}
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Errorf("JWKSFetchFailed after a refresh that published no keys: got %d, want 1", got)
	}
}

func TestSupabaseJWTVerifier_ColdStartWithNoKeysIsUnhealthyUntilKeysArrive(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	jwks.publishNoKeys()
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now)

	assertErrorIs(t, verifier.CheckHealth(t.Context()), auth.ErrJWKSEmptyKeySet)

	jwks.rotate(t, &f.privateKey.PublicKey, f.keyID)
	clock.advance(backoffCap)
	if err := verifier.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth once the endpoint publishes keys: %v", err)
	}
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("Verify once the endpoint publishes keys: %v", err)
	}
}

func TestSupabaseJWTVerifier_MissingSub(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	claims := validClaims(f.issuer, f.audience)
	delete(claims, "sub")

	_, err := verifier.Verify(t.Context(), f.signToken(t, claims))

	assertInvalidTokenReason(t, err, "claim_invalid_sub")
}

func tokenWithLifetime(t *testing.T, f *testJWTFixture, iat time.Time, lifetime time.Duration) string {
	t.Helper()
	claims := validClaims(f.issuer, f.audience)
	claims["iat"], claims["exp"] = iat, iat.Add(lifetime)
	return f.signToken(t, claims)
}

func TestSupabaseJWTVerifier_LifetimeAtMaximumAccepted(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := tokenWithLifetime(t, f, time.Now().Add(-1*time.Minute), time.Hour)

	if _, err := verifier.Verify(t.Context(), token); err != nil {
		t.Fatalf("Verify token with exactly the maximum lifetime: %v", err)
	}
}

func TestSupabaseJWTVerifier_LifetimeOverMaximumRejected(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	_, err := verifier.Verify(t.Context(), tokenWithLifetime(t, f, time.Now().Add(-1*time.Minute), 24*time.Hour))

	assertInvalidTokenReason(t, err, "claim_invalid_iat")
}

func TestSupabaseJWTVerifier_MissingIatRejected(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	claims := validClaims(f.issuer, f.audience)
	delete(claims, "iat")

	_, err := verifier.Verify(t.Context(), f.signToken(t, claims))

	assertInvalidTokenReason(t, err, "claim_invalid_iat")
}

func TestSupabaseJWTVerifier_FutureIatCannotShrinkLifetime(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)

	token := tokenWithLifetime(t, f, time.Now().Add(30*time.Minute), time.Hour)

	if _, err := verifier.Verify(t.Context(), token); err == nil {
		t.Fatal("expected error for forward-dated iat, got nil")
	}
}

func TestSupabaseJWTVerifier_VerifyReportsTokenExpiry(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	claims := validClaims(f.issuer, f.audience)
	exp := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	claims["exp"] = exp

	verified, err := verifier.Verify(t.Context(), f.signToken(t, claims))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.UserID.String() != claims["sub"] {
		t.Errorf("userId: got %q, want %q", verified.UserID.String(), claims["sub"])
	}
	if !verified.ExpiresAt.Equal(exp) {
		t.Errorf("expiry: got %v, want the token's exp %v", verified.ExpiresAt, exp)
	}
}

func TestSupabaseJWTVerifier_CountsStartupAndForcedJWKSFetchFailures(t *testing.T) {
	server, hits := newCountingJWKSServer(t, 0, nil)
	metrics := &countingAuthMetrics{}
	clock := &testClock{}
	verifier := newVerifier(t, server.URL, clock.now, withJWKSMetrics(metrics))
	if got := metrics.jwksFailures.Load(); got != 1 {
		t.Fatalf("after failed startup fetch: JWKSFetchFailed %d, want 1", got)
	}

	for range 5 {
		if _, err := verifier.Verify(t.Context(), "any-token"); err == nil {
			t.Fatal("expected an error while JWKS is down")
		}
	}
	if got, fetches := metrics.jwksFailures.Load(), hits.Load(); got != 2 || fetches != 2 {
		t.Fatalf("after 5 requests during the outage: JWKSFetchFailed %d over %d real fetches, want 2 and 2", got, fetches)
	}
}

func TestSupabaseJWTVerifier_CountsBackgroundJWKSRefreshFailures(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	metrics := &countingAuthMetrics{}
	newVerifier(t, f.jwksServer.URL, time.Now, withJWKSMetrics(metrics), fastBackgroundRefresh)
	if got := metrics.jwksFailures.Load(); got != 0 {
		t.Fatalf("after successful startup fetch: JWKSFetchFailed %d, want 0", got)
	}

	jwks.down.Store(true)

	waitFor(t, 10*time.Second, "failed background refreshes to be counted", func() bool { return metrics.jwksFailures.Load() >= 2 })
}

func TestSupabaseJWTVerifier_VerifyRefusesKeySetPastStalenessBound(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	clock := &testClock{}
	metrics := &countingAuthMetrics{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now, withJWKSMetrics(metrics), fastBackgroundRefresh)
	token := f.validToken(t)

	jwks.down.Store(true)
	waitFor(t, 10*time.Second, "a failed background refresh", func() bool { return metrics.jwksFailures.Load() >= 1 })
	clock.advance(staleAfter + time.Minute)

	_, err := verifier.Verify(t.Context(), token)
	assertErrorIs(t, err, auth.ErrJWKSStale)
	assertVerifierUnavailable(t, err)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	auth.MustMiddleware(verifier, time.Now, silentAuthMetrics{}, auth.DefaultFailureLimits)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("middleware status with a stale key set: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestSupabaseJWTVerifier_VerifyAcceptsAgainOnceKeySetRefreshes(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, clock.now, fastBackgroundRefresh)
	token := f.validToken(t)

	jwks.down.Store(true)
	clock.advance(staleAfter + time.Minute)
	_, err := verifier.Verify(t.Context(), token)
	assertErrorIs(t, err, auth.ErrJWKSStale)

	jwks.down.Store(false)
	waitFor(t, 10*time.Second, "Verify to accept after the endpoint recovers", func() bool {
		_, err := verifier.Verify(t.Context(), token)
		return err == nil
	})
}

type stubRevoker struct {
	revoked bool
	err     error
}

func (s stubRevoker) Revoked(context.Context, ids.UserId, time.Time) (bool, error) {
	return s.revoked, s.err
}

func verifyWithRevoker(t *testing.T, opts ...verifierOption) error {
	t.Helper()
	f := newTestJWTFixture(t)
	verifier := newVerifier(t, f.jwksServer.URL, time.Now, opts...)
	_, err := verifier.Verify(t.Context(), f.validToken(t))
	return err
}

func TestSupabaseJWTVerifier_RevokedTokenRejected(t *testing.T) {
	err := verifyWithRevoker(t, withTokenRevoker(stubRevoker{revoked: true}))
	assertInvalidTokenReason(t, err, "revoked")
}

func TestSupabaseJWTVerifier_DefaultRevokerAcceptsToken(t *testing.T) {
	if err := verifyWithRevoker(t); err != nil {
		t.Fatalf("Verify with default revoker: %v", err)
	}
}

func TestSupabaseJWTVerifier_RevokerErrorIsUnavailableNotInvalid(t *testing.T) {
	err := verifyWithRevoker(t, withTokenRevoker(stubRevoker{err: errors.New("store down")}))
	assertVerifierUnavailable(t, err)
}

type recordingRevoker struct {
	user     ids.UserId
	issuedAt time.Time
}

func (r *recordingRevoker) Revoked(_ context.Context, user ids.UserId, issuedAt time.Time) (bool, error) {
	r.user, r.issuedAt = user, issuedAt
	return false, nil
}

func TestSupabaseJWTVerifier_RevokerSeesTheTokensSubjectAndIssuedAt(t *testing.T) {
	f := newTestJWTFixture(t)
	revoker := &recordingRevoker{}
	verifier := newVerifier(t, f.jwksServer.URL, time.Now, withTokenRevoker(revoker))
	claims := validClaims(f.issuer, f.audience)
	iat := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	claims["iat"] = iat

	if _, err := verifier.Verify(t.Context(), f.signToken(t, claims)); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if revoker.user.String() != claims["sub"] || !revoker.issuedAt.Equal(iat) {
		t.Fatalf("revoker asked about (%s, %v), want the token's (%s, %v)", revoker.user, revoker.issuedAt, claims["sub"], iat)
	}
}

func TestSupabaseJWTVerifier_InvalidSignature(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	f.privateKey = generateRSAKey(t)

	_, err := verifier.Verify(t.Context(), f.validToken(t))

	assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))
}

func TestSupabaseJWTVerifier_RejectsAForeignIssuerOrAudience(t *testing.T) {
	cases := []struct {
		name, claim, value, reason string
	}{
		{"foreign issuer", "iss", "https://other-project.supabase.co/auth/v1", "claim_invalid_iss"},
		{"foreign audience", "aud", "anon", "claim_invalid_aud"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestJWTFixture(t)
			verifier := f.newVerifier(t)
			claims := validClaims(f.issuer, f.audience)
			claims[tc.claim] = tc.value

			_, err := verifier.Verify(t.Context(), f.signToken(t, claims))

			assertInvalidTokenReason(t, err, tc.reason)
		})
	}
}

func TestSupabaseJWTVerifier_LifetimeBoundIsOneHourPlusSkew(t *testing.T) {
	f := newTestJWTFixture(t)
	verifier := f.newVerifier(t)
	iat := time.Now().Add(-1 * time.Minute).Truncate(time.Second)

	if _, err := verifier.Verify(t.Context(), tokenWithLifetime(t, f, iat, time.Hour+5*time.Second)); err != nil {
		t.Fatalf("lifetime of exactly 1h plus the 5s skew: %v", err)
	}
	_, err := verifier.Verify(t.Context(), tokenWithLifetime(t, f, iat, time.Hour+6*time.Second))
	assertInvalidTokenReason(t, err, "claim_invalid_iat")
}

func TestNewSupabaseJWTVerifier_RefusesANilClock(t *testing.T) {
	f := newTestJWTFixture(t)

	verifier, err := auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, testProjectURL, testAudience, nil, random.Real, silentAuthMetrics{}, auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule())

	if !errors.Is(err, auth.ErrJWKSClockRequired) || verifier != nil {
		t.Fatalf("NewSupabaseJWTVerifier(nil clock) = (%v, %v), want ErrJWKSClockRequired and no verifier", verifier, err)
	}
}

func TestNewSupabaseJWTVerifier_RefusesANilJitterSource(t *testing.T) {
	f := newTestJWTFixture(t)

	verifier, err := auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, testProjectURL, testAudience, time.Now, nil, silentAuthMetrics{}, auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule())

	if !errors.Is(err, auth.ErrJWKSJitterRequired) || verifier != nil || !strings.Contains(err.Error(), "NewSupabaseJWTVerifier") {
		t.Fatalf("NewSupabaseJWTVerifier(nil jitter) = (%v, %v), want an error naming the constructor wrapping ErrJWKSJitterRequired and no verifier", verifier, err)
	}
}

func TestSupabaseJWTVerifier_NonPositiveOrNilOptionsKeepTheDefaults(t *testing.T) {
	cases := map[string]verifierOption{
		"zero fetch timeout":        withJWKSFetchTimeout(0),
		"negative fetch timeout":    withJWKSFetchTimeout(-time.Second),
		"zero refresh schedule":     withJWKSBackgroundRefresh(0, 0),
		"negative refresh schedule": withJWKSBackgroundRefresh(-time.Second, -time.Second),
		"zero refresh window":       withJWKSBackgroundRefresh(90*time.Second, 0),
		"negative refresh window":   withJWKSBackgroundRefresh(90*time.Second, -time.Second),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTestJWTFixture(t)
			metrics := &countingAuthMetrics{}
			verifier := newVerifier(t, f.jwksServer.URL, time.Now, opt, withJWKSMetrics(metrics))

			if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if err := verifier.CheckHealth(t.Context()); err != nil {
				t.Fatalf("CheckHealth: %v", err)
			}
			if got := metrics.jwksFailures.Load(); got != 0 {
				t.Fatalf("JWKSFetchFailed %d, want 0: the option broke the startup fetch", got)
			}
		})
	}
}

func TestSupabaseJWTVerifier_NonPositiveRefreshIntervalKeepsTheFifteenMinuteDefault(t *testing.T) {
	f := newTestJWTFixture(t)
	newVerifier(t, f.jwksServer.URL, time.Now, withJWKSBackgroundRefresh(0, 15*time.Minute))
	for _, interval := range []time.Duration{0, -time.Second} {
		_, err := auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, testProjectURL, testAudience, time.Now, random.Real,
			silentAuthMetrics{}, auth.NoopTokenRevoker(), auth.JWKSSchedule{RefreshInterval: interval, RefreshWindow: 15*time.Minute + time.Second})
		if err == nil {
			t.Fatalf("interval %s with a 15m1s window built, want the 15m default interval to refuse the longer window", interval)
		}
		assertErrorSays(t, err, "refresh interval (15m0s)")
	}
}

func TestSupabaseJWTVerifier_StalledBackgroundRefreshFailsWithinTheFetchTimeout(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	metrics := &countingAuthMetrics{}
	newVerifier(t, f.jwksServer.URL, time.Now, withJWKSMetrics(metrics), withJWKSFetchTimeout(200*time.Millisecond), fastBackgroundRefresh)

	jwks.stall.Store(true)

	waitFor(t, 15*time.Second, "a stalled background refresh to time out", func() bool { return metrics.jwksFailures.Load() >= 1 })
}

func warnRecords(t *testing.T, buf *logging.Capture) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		if rec["level"] == "WARN" {
			out = append(out, rec)
		}
	}
	return out
}

func TestSupabaseJWTVerifier_InitialJWKSFetchFailureLogsOneWarn(t *testing.T) {
	buf := logging.CaptureJSON(t, slog.LevelDebug)
	f, jwks := newRotatingFixture(t)
	jwks.down.Store(true)
	verifier := f.newVerifier(t)

	warns := warnRecords(t, buf)
	if len(warns) != 1 {
		t.Fatalf("WARN records after failing initial fetch: got %d, want 1: %s", len(warns), buf.String())
	}
	if got := warns[0]["msg"]; got != "initial JWKS fetch failed, will retry on first request" {
		t.Errorf("WARN msg: got %v", got)
	}
	if got, ok := warns[0]["error"].(string); !ok || got == "" {
		t.Errorf("WARN record missing non-empty error attr: %v", warns[0])
	}

	jwks.down.Store(false)
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("Verify after endpoint recovery: %v", err)
	}
	if got := len(warnRecords(t, buf)); got != 1 {
		t.Fatalf("WARN records after successful fetch: got %d, want still 1: %s", got, buf.String())
	}
}

func TestNewSupabaseJWTVerifier_RefusesMissingMetricsOrRevoker(t *testing.T) {
	f := newTestJWTFixture(t)
	for name, build := range map[string]func(){
		"no metrics": func() {
			_, _ = auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, testProjectURL, testAudience, time.Now, random.Real, nil, auth.NoopTokenRevoker(), auth.DefaultJWKSSchedule())
		},
		"no revoker": func() {
			_, _ = auth.NewSupabaseJWTVerifier(t.Context(), f.jwksServer.URL, testProjectURL, testAudience, time.Now, random.Real, silentAuthMetrics{}, nil, auth.DefaultJWKSSchedule())
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("construction succeeded, want a panic at construction")
				}
			}()
			build()
		})
	}
}

type verifierSettings struct {
	metrics  ports.AuthMetrics
	revoker  ports.TokenRevoker
	schedule auth.JWKSSchedule
}

type verifierOption func(*verifierSettings)

func newVerifierSettings(opts []verifierOption) verifierSettings {
	settings := verifierSettings{
		metrics:  silentAuthMetrics{},
		revoker:  auth.NoopTokenRevoker(),
		schedule: auth.DefaultJWKSSchedule(),
	}
	for _, opt := range opts {
		opt(&settings)
	}
	return settings
}

func withJWKSMetrics(m ports.AuthMetrics) verifierOption {
	return func(s *verifierSettings) { s.metrics = m }
}

func withTokenRevoker(r ports.TokenRevoker) verifierOption {
	return func(s *verifierSettings) { s.revoker = r }
}

func withJWKSFetchTimeout(timeout time.Duration) verifierOption {
	return func(s *verifierSettings) { s.schedule.FetchTimeout = timeout }
}

func withJWKSBackgroundRefresh(interval, window time.Duration) verifierOption {
	return func(s *verifierSettings) {
		s.schedule.RefreshInterval = interval
		s.schedule.RefreshWindow = window
	}
}

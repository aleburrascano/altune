package providers_test

import (
	"altune/go-api/internal/auth"
	"strings"
	"testing"
	"time"
)

func TestJWKSRefresher_BackoffDoublesToCapWithinEqualJitter(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	jwks.down.Store(true)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, auth.WithJWKSClock(clock.now))
	jwks.hits.Store(0)

	steps := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for range 60 {
		steps = append(steps, backoffCap)
	}
	for i, step := range steps {
		_, fetchErr := verifier.Verify(t.Context(), "any-token")
		if got := jwks.hits.Load(); got != int64(i+1) {
			t.Fatalf("attempt %d: JWKS fetches %d, want %d (one per elapsed window)", i+1, got, i+1)
		}
		clock.advance(step/2 - time.Nanosecond)
		_, err := verifier.Verify(t.Context(), "any-token")
		assertErrorSays(t, err, backoffMessage)
		if cause := strings.TrimPrefix(fetchErr.Error(), "fetch JWKS: "); !strings.HasSuffix(err.Error(), cause) {
			t.Fatalf("attempt %d: backoff %q does not carry the last failure %q", i+1, err, cause)
		}
		if got := jwks.hits.Load(); got != int64(i+1) {
			t.Fatalf("attempt %d: fetched just before half of %s", i+1, step)
		}
		clock.advance(step - step/2 + time.Nanosecond)
	}
}

func TestJWKSRefresher_BackoffResetsToBaseAfterSuccess(t *testing.T) {
	f, jwks := newRotatingFixture(t)
	jwks.down.Store(true)
	clock := &testClock{}
	verifier := newVerifier(t, f.jwksServer.URL, auth.WithJWKSClock(clock.now))
	for range 6 {
		_, _ = verifier.Verify(t.Context(), "any-token")
		clock.advance(backoffCap)
	}

	jwks.down.Store(false)
	if _, err := verifier.Verify(t.Context(), f.validToken(t)); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	jwks.down.Store(true)
	f.privateKey, f.keyID = generateRSAKey(t), "made-up"
	clock.advance(unknownKidGap)
	_, _ = verifier.Verify(t.Context(), f.validToken(t))
	jwks.hits.Store(0)
	clock.advance(time.Second)
	_, _ = verifier.Verify(t.Context(), f.validToken(t))

	if got := jwks.hits.Load(); got != 1 {
		t.Fatalf("JWKS fetches one base window after a post-recovery failure: got %d, want 1 (backoff did not reset)", got)
	}
}

package providers

import (
	"altune/go-api/internal/auth/ports"
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// countingAuthMetrics counts JWKSFetchFailed calls; the cache's background
// worker calls it from its own goroutine, hence the atomic.
type countingAuthMetrics struct{ jwksFailures atomic.Int64 }

var _ ports.AuthMetrics = (*countingAuthMetrics)(nil)

func (*countingAuthMetrics) TokenRejected(string) {}
func (*countingAuthMetrics) RequestThrottled()    {}
func (*countingAuthMetrics) VerifierUnavailable() {}
func (m *countingAuthMetrics) JWKSFetchFailed()   { m.jwksFailures.Add(1) }

// Every real failed fetch counts once, the startup fetch and the request-forced
// refresh alike, while requests refused by the backoff do no fetch and add
// nothing, so the counter tracks JWKS reachability rather than request volume.
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

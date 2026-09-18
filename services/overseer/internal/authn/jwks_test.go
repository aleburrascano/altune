package authn_test

import (
	"altune/overseer/internal/authn"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestJWKSThrottlesFetchesDuringOutage is the regression for the throttle-on-attempt
// fix: while the JWKS endpoint is failing, a flood of unknown-kid tokens must trigger
// at most one upstream fetch per throttle window. Before the fix the throttle advanced
// only on a *successful* fetch, so a failing endpoint was re-hit on every token —
// turning the owner guard into an amplifier against Supabase during an outage.
func TestJWKSThrottlesFetchesDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError) // JWKS outage
	}))
	defer srv.Close()
	v := authn.New(srv.URL, testIssuer, "", srv.Client())

	key := genKey(t)
	// A flood of unknown-kid tokens, each of which asks the guard to refresh the JWKS.
	for i := 0; i < 50; i++ {
		token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Fatal("token verified during JWKS outage, want error")
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches during outage = %d, want 1 (throttled on attempt, not success)", got)
	}
}

// TestJWKSThrottlesConcurrentFloodDuringOutage proves the throttle holds under a
// concurrent flood: recording the attempt under the lock (before releasing to fetch)
// means only the first caller in a window fetches even when a slow/failing endpoint
// leaves the fetch in flight, so concurrent unknown-kid tokens cannot each open their
// own upstream request.
func TestJWKSThrottlesConcurrentFloodDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError) // JWKS outage
	}))
	defer srv.Close()
	v := authn.New(srv.URL, testIssuer, "", srv.Client())

	key := genKey(t)
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			token := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
			if _, err := v.Verify(context.Background(), token); err == nil {
				t.Error("token verified during JWKS outage, want error")
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches during concurrent outage = %d, want 1 (throttled on attempt)", got)
	}
}

// TestJWKSRefetchesAfterSuccess confirms the throttle does not break the happy path:
// a first valid token fetches the key set and verifies, and a later unknown kid within
// the window is served from cache (fails closed) without a second fetch.
func TestJWKSRefetchesAfterSuccess(t *testing.T) {
	key := genKey(t)
	var hits int32
	base := jwksServer(t, "kid-1", key)
	defer base.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	v := authn.New(srv.URL, testIssuer, "", srv.Client())

	valid := es256Token(t, key, "kid-1", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), valid); err != nil {
		t.Fatalf("valid token failed to verify: %v", err)
	}
	unknown := es256Token(t, key, "kid-unknown", validClaims("owner-sub"))
	if _, err := v.Verify(context.Background(), unknown); err == nil {
		t.Fatal("unknown-kid token verified, want error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("jwks fetches = %d, want 1 (cached after first success, unknown kid throttled)", got)
	}
}

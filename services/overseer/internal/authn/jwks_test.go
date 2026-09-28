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

func TestJWKSThrottlesFetchesDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := authn.New(srv.URL, testIssuer, "", srv.Client())

	key := genKey(t)
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

func TestJWKSThrottlesConcurrentFloodDuringOutage(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
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

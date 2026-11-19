package middleware_test

import (
	"altune/go-api/internal/auth"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func clientAddr(i int) string {
	return fmt.Sprintf("198.18.%d.%d:4444", i/250, i%250+1)
}

func failingVerifier() auth.VerifierFunc {
	return func(context.Context, string) (auth.VerifiedToken, error) {
		return auth.VerifiedToken{}, &auth.InvalidTokenError{Reason: auth.ReasonSignatureInvalid}
	}
}

func limitedMiddleware(limits auth.FailureLimits) (http.Handler, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	next, _ := noopHandler()
	return auth.Middleware(failingVerifier(), auth.WithFailureLimits(limits, clock.now))(next), clock
}

func admitted(handler http.Handler, addr string) bool {
	return serveBearer(handler, addr, "garbage").Code != http.StatusTooManyRequests
}

func TestFailureThrottle_TrackedClientsStayBounded(t *testing.T) {
	handler, _ := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 8})

	for i := range 1000 {
		if !admitted(handler, clientAddr(i)) {
			t.Fatalf("fresh client %d refused", i)
		}
	}

	first := admitted(handler, clientAddr(0))
	second := admitted(handler, clientAddr(0))
	if !first || !second {
		t.Fatal("the first client still holds its spent token after 999 newer clients: tracked clients grew past the bound")
	}
}

func TestFailureThrottle_EvictionDropsToTheLowWaterMark(t *testing.T) {
	limits := auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100}
	handler, _ := limitedMiddleware(limits)
	const keepers, spenders = 90, 10
	for i := range keepers {
		admitted(handler, clientAddr(i))
		admitted(handler, clientAddr(i))
	}
	for i := keepers; i < keepers+spenders; i++ {
		admitted(handler, clientAddr(i))
	}

	admitted(handler, "203.0.113.200:1")

	forgotten := 0
	for i := keepers; i < keepers+spenders; i++ {
		admitted(handler, clientAddr(i))
		if admitted(handler, clientAddr(i)) {
			forgotten++
		}
	}
	if forgotten != spenders {
		t.Fatalf("after one overflow, %d of the %d least-penalised clients were forgotten, want all %d (eviction down to low water, not one at a time)", forgotten, spenders, spenders)
	}
}

func TestFailureThrottle_RefilledClientsArePrunedBeforeEvictingPenalisedOnes(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 1, Refill: time.Minute, MaxClients: 2})

	admitted(handler, "203.0.113.1:1")
	clock.advance(time.Minute)
	admitted(handler, "203.0.113.2:1")
	admitted(handler, "203.0.113.3:1")

	if admitted(handler, "203.0.113.2:1") {
		t.Fatal("penalised client was evicted in favour of pruning a refilled one")
	}
}

func TestFailureThrottle_ExhaustedClientSurvivesEvictionChurn(t *testing.T) {
	limits := auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 50}
	handler, _ := limitedMiddleware(limits)
	for i := range limits.MaxClients - 1 {
		admitted(handler, clientAddr(i))
	}
	const attacker = "203.0.113.66:1"
	admitted(handler, attacker)
	admitted(handler, attacker)
	if admitted(handler, attacker) {
		t.Fatal("attacker not exhausted before churn")
	}

	for i := range 500 {
		admitted(handler, clientAddr(1000+i))
	}

	if admitted(handler, attacker) {
		t.Fatal("exhausted client got a fresh burst after eviction churn")
	}
}

package auth_test

import (
	"altune/go-api/internal/auth"
	"context"
	"fmt"
	"log/slog"
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

func TestFailureThrottle_NonPositiveLimitsAndNilClockKeepTheDefaults(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	cases := []struct {
		name   string
		limits auth.FailureLimits
		now    func() time.Time
		burst  int
	}{
		{"all zero", auth.FailureLimits{}, clock.now, 20},
		{"zero refill", auth.FailureLimits{Burst: 3, MaxClients: 10}, clock.now, 3},
		{"negative refill", auth.FailureLimits{Burst: 3, Refill: -time.Second, MaxClients: 10}, clock.now, 3},
		{"zero max clients", auth.FailureLimits{Burst: 3, Refill: time.Hour}, clock.now, 3},
		{"negative burst", auth.FailureLimits{Burst: -1, Refill: time.Hour, MaxClients: 10}, clock.now, 20},
		{"nil clock", auth.FailureLimits{Burst: 3, Refill: time.Hour, MaxClients: 10}, nil, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, _ := noopHandler()
			handler := auth.Middleware(failingVerifier(), auth.WithFailureLimits(tc.limits, tc.now))(next)
			codes := map[int]int{}
			for i := range tc.burst + 5 {
				admitted(handler, clientAddr(i))
				codes[serveBearer(handler, "203.0.113.7:1", "garbage").Code]++
			}
			if codes[http.StatusUnauthorized] != tc.burst || codes[http.StatusTooManyRequests] != 5 {
				t.Fatalf("%d failed attempts from one client: %v, want %d x 401 then 5 x 429", tc.burst+5, codes, tc.burst)
			}
		})
	}
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

func TestFailureThrottle_PruningRefilledClientsSparesAPartlySpentOneFromLaterEviction(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100})
	for i := range 15 {
		admitted(handler, clientAddr(i))
	}
	clock.advance(2 * time.Hour)
	const partlySpent = "203.0.113.9:1"
	admitted(handler, partlySpent)
	clock.advance(30 * time.Minute)
	for i := range 84 {
		admitted(handler, clientAddr(100+i))
	}
	for i := range 11 {
		admitted(handler, clientAddr(200+i))
	}

	if !admitted(handler, partlySpent) || admitted(handler, partlySpent) {
		t.Fatal("the partly spent client got a fresh burst: it was evicted while 15 refilled clients could have been pruned")
	}
}

func TestFailureThrottle_NoClientIsForgottenBelowCapacity(t *testing.T) {
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 100})
	const early = "203.0.113.10:1"
	admitted(handler, early)
	clock.advance(30 * time.Minute)
	for i := range 94 {
		admitted(handler, clientAddr(i))
	}

	if !admitted(handler, early) || admitted(handler, early) {
		t.Fatal("with 95 of 100 slots in use, the early client lost its spent token: a client was evicted below capacity")
	}
}

func churnAddr(i int) string {
	return fmt.Sprintf("10.%d.%d.%d:4444", i>>16&255, i>>8&255, i&255)
}

func BenchmarkFailureThrottle_BenignChurn(b *testing.B) { benchmarkChurn(b, 3*time.Second) }

func BenchmarkFailureThrottle_AttackChurn(b *testing.B) { benchmarkChurn(b, 0) }

func benchmarkChurn(b *testing.B, step time.Duration) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(prev) })
	handler, clock := limitedMiddleware(auth.FailureLimits{Burst: 20, Refill: 3 * time.Second, MaxClients: 10_000})
	var worst time.Duration
	for i := 0; b.Loop(); i++ {
		clock.advance(step)
		start := time.Now()
		serveBearer(handler, churnAddr(i), "garbage")
		worst = max(worst, time.Since(start))
	}
	b.ReportMetric(float64(worst.Microseconds()), "worst-µs")
}

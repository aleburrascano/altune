package middleware

import (
	"strconv"
	"testing"
	"time"
)

func TestFailureThrottle_TrackedClientsStayBounded(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	limits := FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 8}
	throttle := newFailureThrottle(limits, clock.now)

	for i := range 1000 {
		if _, _, ok := throttle.admit("client-" + strconv.Itoa(i)); !ok {
			t.Fatalf("fresh client %d refused", i)
		}
	}
	if got := len(throttle.clients); got > limits.MaxClients {
		t.Fatalf("tracked clients: got %d, want at most %d", got, limits.MaxClients)
	}
}

func TestFailureThrottle_EvictionScanIsAmortised(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	limits := FailureLimits{Burst: 1, Refill: time.Hour, MaxClients: 100}
	throttle := newFailureThrottle(limits, clock.now)
	for i := range limits.MaxClients {
		throttle.admit("client-" + strconv.Itoa(i))
	}

	throttle.admit("overflow")
	if got := len(throttle.clients); got > limits.MaxClients-limits.MaxClients/10+1 {
		t.Fatalf("after overflow: %d tracked clients, want eviction down to low water", got)
	}
}

func TestFailureThrottle_RefilledClientsArePrunedBeforeEvictingPenalisedOnes(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	limits := FailureLimits{Burst: 1, Refill: time.Minute, MaxClients: 2}
	throttle := newFailureThrottle(limits, clock.now)

	throttle.admit("idle")
	clock.advance(time.Minute)
	throttle.admit("attacker")
	throttle.admit("newcomer")

	if _, _, ok := throttle.admit("attacker"); ok {
		t.Fatal("penalised client was evicted in favour of pruning a refilled one")
	}
}

func TestFailureThrottle_ExhaustedClientSurvivesEvictionChurn(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	limits := FailureLimits{Burst: 2, Refill: time.Hour, MaxClients: 50}
	throttle := newFailureThrottle(limits, clock.now)
	for i := range limits.MaxClients - 1 {
		throttle.admit("client-" + strconv.Itoa(i))
	}
	throttle.admit("attacker")
	throttle.admit("attacker")
	if _, _, ok := throttle.admit("attacker"); ok {
		t.Fatal("attacker not exhausted before churn")
	}

	for i := range 500 {
		throttle.admit("churn-" + strconv.Itoa(i))
	}

	if _, _, ok := throttle.admit("attacker"); ok {
		t.Fatal("exhausted client got a fresh burst after eviction churn")
	}
	if got := len(throttle.clients); got > limits.MaxClients {
		t.Fatalf("tracked clients: got %d, want at most %d", got, limits.MaxClients)
	}
}

package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestClientKey(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{name: "public peer", remoteAddr: "203.0.113.7:5000", want: "203.0.113.7"},
		{name: "public peer ignores spoofed forwarded header", remoteAddr: "203.0.113.7:5000", forwarded: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "private proxy peer uses last forwarded hop", remoteAddr: "172.18.0.3:5000", forwarded: []string{"10.9.9.9, 198.51.100.1"}, want: "198.51.100.1"},
		{name: "loopback proxy uses last forwarded header line", remoteAddr: "127.0.0.1:5000", forwarded: []string{"1.1.1.1", "198.51.100.2"}, want: "198.51.100.2"},
		{name: "private peer with garbage forwarded falls back to peer", remoteAddr: "172.18.0.3:5000", forwarded: []string{"not-an-ip"}, want: "172.18.0.3"},
		{name: "ipv6 keyed by /64", remoteAddr: "[2001:db8:1:2:aaaa::1]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv6 neighbours in the same /64 share a key", remoteAddr: "[2001:db8:1:2:bbbb::9]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv4-mapped ipv6 keyed as ipv4", remoteAddr: "[::ffff:203.0.113.7]:5000", want: "203.0.113.7"},
		{name: "unparseable remote addr used verbatim", remoteAddr: "pipe", want: "pipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := clientKey(req); got != tt.want {
				t.Errorf("clientKey: got %q, want %q", got, tt.want)
			}
		})
	}
}

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

// Under a flood of distinct penalised addresses a full-map scan must not run
// on every new client, or the throttle itself becomes the amplifier.
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

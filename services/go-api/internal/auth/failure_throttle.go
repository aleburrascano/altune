package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type FailureLimits struct {
	Burst      int
	Refill     time.Duration
	MaxClients int
}

var DefaultFailureLimits = FailureLimits{
	Burst:      20,
	Refill:     3 * time.Second,
	MaxClients: 10_000,
}

type failureThrottle struct {
	mu      sync.Mutex
	limits  FailureLimits
	now     func() time.Time
	clients map[string]*rate.Limiter
}

func newFailureThrottle(limits FailureLimits, now func() time.Time) *failureThrottle {
	return &failureThrottle{limits: limits, now: now, clients: make(map[string]*rate.Limiter)}
}

type attempt struct {
	throttle    *failureThrottle
	reservation *rate.Reservation
	reservedAt  time.Time
}

func (t *failureThrottle) admit(key string) (attempt, time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	res := t.limiterFor(key, now).ReserveN(now, 1)
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return attempt{}, delay, false
	}
	return attempt{throttle: t, reservation: res, reservedAt: now}, 0, true
}

func (a attempt) succeeded() {
	a.throttle.mu.Lock()
	defer a.throttle.mu.Unlock()
	a.reservation.CancelAt(a.reservedAt)
}

func (t *failureThrottle) limiterFor(key string, now time.Time) *rate.Limiter {
	if lim, ok := t.clients[key]; ok {
		return lim
	}
	t.makeRoom(now)
	lim := rate.NewLimiter(rate.Every(t.limits.Refill), t.limits.Burst)
	t.clients[key] = lim
	return lim
}

func (t *failureThrottle) makeRoom(now time.Time) {
	if len(t.clients) < t.limits.MaxClients {
		return
	}
	for key, lim := range t.clients {
		if lim.TokensAt(now) >= float64(t.limits.Burst) {
			delete(t.clients, key)
		}
	}
	lowWater := t.limits.MaxClients - max(1, t.limits.MaxClients/10)
	for key := range t.clients {
		if len(t.clients) <= lowWater {
			return
		}
		delete(t.clients, key)
	}
}

func ClientKey(r *http.Request) string { return clientKey(r) }

func clientKey(r *http.Request) string {
	peer := parseIP(hostOnly(r.RemoteAddr))
	if forwarded, ok := forwardedClient(r, peer); ok {
		return prefixKey(forwarded)
	}
	if !peer.IsValid() {
		return r.RemoteAddr
	}
	return prefixKey(peer)
}

func forwardedClient(r *http.Request, peer netip.Addr) (netip.Addr, bool) {
	if !peer.IsValid() || (!peer.IsPrivate() && !peer.IsLoopback()) {
		return netip.Addr{}, false
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return netip.Addr{}, false
	}
	hops := strings.Split(values[len(values)-1], ",")
	addr := parseIP(strings.TrimSpace(hops[len(hops)-1]))
	return addr, addr.IsValid()
}

func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

func parseIP(s string) netip.Addr {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap().WithZone("")
}

func prefixKey(addr netip.Addr) string {
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

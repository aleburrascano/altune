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

// FailureLimits bounds how many token verifications one client may fail. A
// client gets Burst failed attempts up front and earns one more every Refill;
// successful verifications cost nothing. MaxClients caps the tracked-client map
// so a caller rotating addresses cannot grow memory without bound.
type FailureLimits struct {
	Burst      int
	Refill     time.Duration
	MaxClients int
}

// DefaultFailureLimits leaves a real client ample room for an expired-token
// retry loop while holding a single address to a few verifications per minute.
var DefaultFailureLimits = FailureLimits{
	Burst:      20,
	Refill:     3 * time.Second,
	MaxClients: 10_000,
}

// failureThrottle is a token bucket per client key. Each verification attempt
// reserves a token before any verification work runs, so concurrent attempts
// cannot overshoot the bucket; a successful verification returns its token.
type failureThrottle struct {
	mu      sync.Mutex
	limits  FailureLimits
	now     func() time.Time
	clients map[string]*rate.Limiter
}

func newFailureThrottle(limits FailureLimits, now func() time.Time) *failureThrottle {
	return &failureThrottle{limits: limits, now: now, clients: make(map[string]*rate.Limiter)}
}

// attempt is one admitted verification. Succeeded refunds its token; an
// attempt that is never marked succeeded stays charged as a failure.
type attempt struct {
	throttle    *failureThrottle
	reservation *rate.Reservation
	reservedAt  time.Time
}

// admit reserves a failure token for key. When the bucket is empty it returns
// ok=false and how long until the next token, charging nothing.
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

// succeeded refunds the token by cancelling at the instant the reservation was
// made. Cancelling at a later clock reading is a no-op — rate.CancelAt drops
// any reservation whose time-to-act has already passed — so re-reading the
// clock here would silently charge every successful verification and throttle a
// legitimate caller after Burst rapid requests.
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

// makeRoom drops clients whose bucket has fully refilled (they carry no
// state worth keeping), then evicts arbitrary penalised ones until the map is
// down to lowWater. Draining below MaxClients, not just to it, keeps the full
// scan amortised: a flood of new addresses cannot force one per request.
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

// clientKey identifies the caller for throttling. X-Forwarded-For is trusted
// only when the direct peer is a private or loopback address (the Caddy
// reverse proxy on the Docker network); a public peer is keyed by its own
// address so it cannot rotate a spoofed header to dodge the limit. IPv6
// callers are keyed by /64, the smallest block one subscriber usually holds.
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

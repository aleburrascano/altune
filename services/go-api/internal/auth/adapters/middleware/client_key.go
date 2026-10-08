package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

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

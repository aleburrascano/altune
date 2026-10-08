package providerhttp

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const liveMaxConnsPerHost = 8

var ErrNonPublicAddr = errors.New("provider request refused: address is not public")

var nonPublicProviderPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

func isNonPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return true
	}
	for _, prefix := range nonPublicProviderPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func RefuseNonPublicDial(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil || isNonPublicAddr(addrPort.Addr()) {
		return ErrNonPublicAddr
	}
	return nil
}

func baseTransport() http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	c := t.Clone()
	c.MaxConnsPerHost = liveMaxConnsPerHost
	c.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   RefuseNonPublicDial,
	}).DialContext
	return c
}

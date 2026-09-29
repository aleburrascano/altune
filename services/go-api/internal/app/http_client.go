package app

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"syscall"
	"time"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
)

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

const liveMaxConnsPerHost = 8

var errNonPublicProviderAddr = errors.New("provider request refused: address is not public")

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

func isNonPublicProviderAddr(addr netip.Addr) bool {
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

func refuseNonPublicDial(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil || isNonPublicProviderAddr(addrPort.Addr()) {
		return errNonPublicProviderAddr
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
		Control:   refuseNonPublicDial,
	}).DialContext
	return c
}

func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return providermetrics.NewCountingTransport(base)
}

var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(NewLiveTransport())
})

type clientFactory struct {
	transport http.RoundTripper
}

func newClientFactory(transport http.RoundTripper) clientFactory {
	if transport == nil {
		return clientFactory{transport: sharedLiveTransport()}
	}
	return clientFactory{transport: transport}
}

func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.transport}
}

func (f clientFactory) chart() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout, Transport: f.transport}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.transport
}

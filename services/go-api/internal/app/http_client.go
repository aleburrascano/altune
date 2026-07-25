package app

import (
	"net/http"
	"time"
)

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

var defaultLiveTransport = NewLiveTransport()

type clientFactory struct {
	transport http.RoundTripper
}

func (f clientFactory) clientTransport() http.RoundTripper {
	if f.transport != nil {
		return f.transport
	}
	return defaultLiveTransport
}

func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.clientTransport()}
}

func (f clientFactory) chart() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout, Transport: f.clientTransport()}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.clientTransport()
}

func newDiscoveryClient() *http.Client {
	return clientFactory{}.discovery()
}

func newChartClient() *http.Client {
	return clientFactory{}.chart()
}

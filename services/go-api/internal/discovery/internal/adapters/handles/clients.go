package handles

import "net/http"

type Clients struct {
	ProviderCalls *http.Client
	ChartCalls    *http.Client
	Transport     http.RoundTripper
}

func NewClients(d Deps) Clients {
	return Clients{
		ProviderCalls: &http.Client{Timeout: d.ProviderTimeout, Transport: d.Transport},
		ChartCalls:    &http.Client{Timeout: d.ChartTimeout, Transport: d.Transport},
		Transport:     d.Transport,
	}
}

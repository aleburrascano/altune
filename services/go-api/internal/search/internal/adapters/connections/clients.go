package connections

import (
	"altune/go-api/internal/platform/outbound"
	"net/http"
)

type Clients struct {
	ProviderCalls *http.Client
	ChartCalls    *http.Client
	Transport     http.RoundTripper
}

func NewClients(d Deps) Clients {
	return Clients{
		ProviderCalls: outbound.MustNewClient(d.ProviderTimeout, d.Transport),
		ChartCalls:    outbound.MustNewClient(d.ChartTimeout, d.Transport),
		Transport:     d.Transport,
	}
}

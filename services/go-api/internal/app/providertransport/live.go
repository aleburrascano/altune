package providertransport

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/telemetry"
	"fmt"
	"net/http"
)

func New(metrics *telemetry.Registry) (http.RoundTripper, error) {
	live, err := outbound.NewLiveTransport(search.ProviderRetryRecorder(metrics))
	if err != nil {
		return nil, fmt.Errorf("provider transport: %w", err)
	}
	return search.NewCountingTransport(live, metrics), nil
}

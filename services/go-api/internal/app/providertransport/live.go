package providertransport

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"fmt"
	"net/http"
	"sync"
)

var Live = sync.OnceValues(func() (http.RoundTripper, error) {
	live, err := outbound.NewLiveTransport(search.RecordRetry)
	if err != nil {
		return nil, fmt.Errorf("provider transport: %w", err)
	}
	return search.NewCountingTransport(live), nil
})

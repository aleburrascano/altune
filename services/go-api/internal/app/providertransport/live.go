package providertransport

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search"
	"net/http"
	"sync"
)

var Live = sync.OnceValue(func() http.RoundTripper {
	return search.NewCountingTransport(outbound.NewLiveTransport(search.RecordRetry))
})

package outbound

import (
	"altune/go-api/internal/platform/outbound/internal/pacing"
	"altune/go-api/internal/platform/outbound/internal/retry"
	"altune/go-api/internal/shared/clock"
	"errors"
	"fmt"
	"net/http"
)

var errRetryRecorderRequired = errors.New("live transport needs a retry recorder, pass a no-op func to switch it off")

func NewLiveTransport(onRetry func(host string)) (http.RoundTripper, error) {
	if onRetry == nil {
		return nil, errRetryRecorderRequired
	}
	o := retry.SystemOptions()
	o.OnRetry = onRetry
	pacer, err := pacing.New(clock.Now, clock.Sleep)
	if err != nil {
		return nil, fmt.Errorf("build the live transport pacer: %w", err)
	}
	transport, err := retry.New(baseTransport(), pacer, o)
	if err != nil {
		return nil, fmt.Errorf("build the live transport: %w", err)
	}
	return transport, nil
}

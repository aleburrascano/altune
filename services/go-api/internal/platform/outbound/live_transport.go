package outbound

import (
	"altune/go-api/internal/platform/outbound/internal/pacing"
	"altune/go-api/internal/platform/outbound/internal/retry"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrRetryRecorderRequired = errors.New("live transport needs a retry recorder, pass NoRetryRecorder to switch it off")

func NoRetryRecorder(string) {}

func NewLiveTransport(onRetry func(host string)) http.RoundTripper {
	if onRetry == nil {
		return refusedTransport{err: ErrRetryRecorderRequired}
	}
	o := retry.SystemOptions()
	o.OnRetry = onRetry
	pacer, err := pacing.New(time.Now)
	if err != nil {
		return refusedTransport{err: fmt.Errorf("build the live transport pacer: %w", err)}
	}
	transport, err := retry.New(baseTransport(), pacer, o)
	if err != nil {
		return refusedTransport{err: fmt.Errorf("build the live transport: %w", err)}
	}
	return transport
}

type refusedTransport struct{ err error }

func (r refusedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, r.err
}

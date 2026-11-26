package outbound

import (
	"altune/go-api/internal/platform/outbound/internal/pacing"
	"altune/go-api/internal/platform/outbound/internal/retry"
	"fmt"
	"net/http"
	"time"
)

type LiveOption func(*retry.Options)

func WithRetryRecorder(record func(host string)) LiveOption {
	return func(o *retry.Options) { o.OnRetry = record }
}

func NewLiveTransport(opts ...LiveOption) http.RoundTripper {
	o := retry.Options{}
	for _, opt := range opts {
		opt(&o)
	}
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

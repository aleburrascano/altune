package outbound

import (
	"altune/go-api/internal/platform/outbound/internal/retry"
	"net/http"
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
	return retry.New(baseTransport(), o)
}

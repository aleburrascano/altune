package outbound

import (
	"errors"
	"net/http"
	"time"
)

var ErrClientTimeoutRequired = errors.New("outbound client needs a positive timeout")

func NewClient(timeout time.Duration, transport http.RoundTripper) (*http.Client, error) {
	if timeout <= 0 {
		return nil, ErrClientTimeoutRequired
	}
	if transport == nil {
		transport = baseTransport()
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

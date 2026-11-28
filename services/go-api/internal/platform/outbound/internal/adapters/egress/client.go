package egress

import (
	"errors"
	"net"
	"net/http"
	"time"
)

var ErrClientTimeoutRequired = errors.New("outbound client needs a positive timeout")

func NewClient(timeout time.Duration, transport http.RoundTripper, opts ...clientOption) (*http.Client, error) {
	if timeout <= 0 {
		return nil, ErrClientTimeoutRequired
	}
	var cfg clientConfig
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.bodyCap > 0 {
		transport = newBodyCapTransport(transport, cfg.bodyCap)
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func MustNewClient(timeout time.Duration, transport http.RoundTripper, opts ...clientOption) *http.Client {
	client, err := NewClient(timeout, transport, opts...)
	if err != nil {
		panic(err)
	}
	return client
}

func DefaultTransport() http.RoundTripper {
	return http.DefaultTransport
}

type TransportOptions struct {
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
}

func NewTransport(o TransportOptions) *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: o.DialTimeout}).DialContext,
		TLSHandshakeTimeout:   o.TLSHandshakeTimeout,
		ResponseHeaderTimeout: o.ResponseHeaderTimeout,
		IdleConnTimeout:       o.IdleConnTimeout,
		MaxIdleConns:          o.MaxIdleConns,
		MaxIdleConnsPerHost:   o.MaxIdleConnsPerHost,
	}
}

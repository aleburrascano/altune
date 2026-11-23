package server

import (
	"context"
	"net"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

func New(lifecycle context.Context, addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(lifecycle) },
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
}

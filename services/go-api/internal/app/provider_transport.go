package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/egress"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

func warnIgnoredStartupOptIns(ctx context.Context, cfg *config.Config) {
	if !cfg.TestAuthEnabled() {
		warnIgnoredOptIn(ctx, cfg, "TEST_AUTH_ENABLED", cfg.TestAuthOptIn)
	}
}

func warnIgnoredOptIn(ctx context.Context, cfg *config.Config, flag string, optedIn bool) {
	if !optedIn {
		return
	}
	slog.WarnContext(ctx, "startup opt-in ignored: honoured only when ENV is explicitly development or test",
		"flag", flag, "env", cfg.Env)
}

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return discovery.NewCountingTransport(base)
}

var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(egress.NewLiveTransport(egress.WithRetryRecorder(discovery.RecordRetry)))
})

type clientFactory struct {
	transport http.RoundTripper
}

func newClientFactory(transport http.RoundTripper) clientFactory {
	if transport == nil {
		return clientFactory{transport: sharedLiveTransport()}
	}
	return clientFactory{transport: transport}
}

func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.transport}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.transport
}

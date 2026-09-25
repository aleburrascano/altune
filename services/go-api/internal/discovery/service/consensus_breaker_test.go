package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func failingConsensusProvider(calls *atomic.Int32) ConsensusProvider {
	return ConsensusProvider{
		Name:     "lastfm",
		Provider: domain.ProviderLastFM,
		Fetcher: func(context.Context, string) ([]domain.SearchResult, error) {
			calls.Add(1)
			return nil, fmt.Errorf("get https://x/?api_key=sekrit: %w", context.DeadlineExceeded)
		},
	}
}

func TestConsensusOpensBreakerAndStopsCallingFailingProvider(t *testing.T) {
	var calls atomic.Int32
	breaker := NewCircuitBreaker()
	svc := NewConsensusService(
		[]ConsensusProvider{failingConsensusProvider(&calls), consensusProvider("itunes", "A")},
		WithConsensusCircuitBreaker(breaker),
	)

	for range failureThreshold + 3 {
		svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)
	}

	if got := calls.Load(); got != failureThreshold {
		t.Errorf("failing provider called %d times, want %d before the circuit opens", got, failureThreshold)
	}
	if breaker.AllowRequest(domain.ProviderLastFM) {
		t.Error("circuit for the failing provider should be open")
	}
}

func TestConsensusLogsFailingProviderByNameWithoutSecrets(t *testing.T) {
	buf := captureLogs(t)
	var calls atomic.Int32
	svc := NewConsensusService([]ConsensusProvider{failingConsensusProvider(&calls)})

	svc.BuildConsensus(context.Background(), "Artist", domain.ProviderDeezer, "", nil)

	out := buf.String()
	if !strings.Contains(out, "consensus.provider_failed") || !strings.Contains(out, `"provider":"lastfm"`) {
		t.Errorf("expected a provider_failed log naming lastfm, got %s", out)
	}
	if strings.Contains(out, "sekrit") {
		t.Errorf("log leaked a secret: %s", out)
	}
}

package connections_test

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/service/useractivity"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/queuetimeout"
	"context"
	"expvar"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func depsWithProviderTimeout(timeout time.Duration) connections.Deps {
	return connections.Deps{
		Lifecycle:       context.Background(),
		Background:      &background.Tracker{},
		Tracker:         &background.Tracker{},
		Logger:          slog.Default(),
		Transport:       http.DefaultTransport,
		Activity:        useractivity.NoopActivityFeed{},
		ClientEvents:    handler.NoClientEvents{},
		CacheOps:        new(expvar.Map),
		ProviderTimeout: timeout,
		ChartTimeout:    time.Second,
	}
}

func TestDeps_RefusesAProviderTimeoutThatCannotOutlastTheLongestRateLimitQueueWait(t *testing.T) {
	for _, timeout := range []time.Duration{queuetimeout.MaxWait / 2, queuetimeout.MaxWait} {
		if err := depsWithProviderTimeout(timeout).MissingHandles(); err == nil || !strings.Contains(err.Error(), "ProviderTimeout") {
			t.Errorf("ProviderTimeout %v: MissingHandles = %v, want a refusal naming ProviderTimeout", timeout, err)
		}
	}
	if err := depsWithProviderTimeout(queuetimeout.MaxWait + time.Second).MissingHandles(); err != nil {
		t.Errorf("a ProviderTimeout past the queue wait was refused: %v", err)
	}
}

func TestDeps_MissingHandlesNamesTheTrackerWhenItIsAbsent(t *testing.T) {
	err := connections.Deps{}.MissingHandles()

	if err == nil || !strings.Contains(err.Error(), "Tracker") {
		t.Fatalf("MissingHandles = %v, want an error naming Tracker", err)
	}
}

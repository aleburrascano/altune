package search_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/queuetimeout"
	"altune/go-api/internal/shared/telemetry"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func depsWithProviderTimeout(timeout time.Duration) search.Deps {
	return search.Deps{
		Background:      &background.Tracker{},
		Tracker:         &background.Tracker{},
		Logger:          slog.Default(),
		Transport:       http.DefaultTransport,
		Activity:        search.NoActivityFeed{},
		ClientEvents:    search.NoClientEvents{},
		CacheOps:        telemetry.NewRegistry().LabeledCounter("discovery_cache_ops"),
		ProviderTimeout: timeout,
		ChartTimeout:    time.Second,
	}
}

func TestDeps_RefusesAProviderTimeoutThatCannotOutlastTheLongestRateLimitQueueWait(t *testing.T) {
	for _, timeout := range []time.Duration{queuetimeout.MaxWait / 2, queuetimeout.MaxWait} {
		if _, err := search.New(t.Context(), depsWithProviderTimeout(timeout)); err == nil || !strings.Contains(err.Error(), "ProviderTimeout") {
			t.Errorf("ProviderTimeout %v: New = %v, want a refusal naming ProviderTimeout", timeout, err)
		}
	}
	if _, err := search.New(t.Context(), depsWithProviderTimeout(queuetimeout.MaxWait+time.Second)); err != nil {
		t.Errorf("a ProviderTimeout past the queue wait was refused: %v", err)
	}
}

func TestDeps_MissingHandlesNamesTheTrackerWhenItIsAbsent(t *testing.T) {
	_, err := search.New(t.Context(), search.Deps{})

	if err == nil || !strings.Contains(err.Error(), "Tracker") {
		t.Fatalf("New = %v, want an error naming Tracker", err)
	}
}

package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/runloop"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestApplyStartupSwitches_AcquisitionPausedPausesWiredScheduler(t *testing.T) {
	a := &App{
		cfg: &config.Config{
			MusicDir:                t.TempDir(),
			YtMusicEnabled:          true,
			AcquisitionFixtureOptIn: true,
			AcquisitionPaused:       true,
		},
		sem: make(chan struct{}, 1),
	}

	wired, err := a.wireCatalog(t.Context(), nil, &discovery.Discovery{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if wired.scheduler == nil {
		t.Fatal("scheduler not wired, cannot assert pause")
	}
	a.scheduler = wired.scheduler

	if err := a.applyStartupSwitches(t.Context()); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}

	if !wired.scheduler.Status().Paused {
		t.Fatal("Status().Paused = false, want true after ACQUISITION_PAUSED=true")
	}
}

func TestApplyStartupSwitches_AcquisitionPausedNoSchedulerIsNoop(t *testing.T) {
	a := &App{cfg: &config.Config{AcquisitionPaused: true}}

	if err := a.applyStartupSwitches(t.Context()); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}
}

func TestApplyStartupSwitches_DisabledJobsDisableEachNamedJob(t *testing.T) {
	a := &App{cfg: &config.Config{DisabledJobs: []string{"acquisition prune", "stream recovery"}}}

	if err := a.applyStartupSwitches(t.Context()); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}

	for _, name := range []string{"acquisition prune", "stream recovery"} {
		h := findJobHealth(t, a.JobHealth(), name)
		if h.Enabled {
			t.Errorf("job %q enabled, want disabled via DISABLED_JOBS", name)
		}
	}
}

func TestApplyStartupSwitches_DisabledJobsUnknownNameFailsStartup(t *testing.T) {
	a := &App{cfg: &config.Config{DisabledJobs: []string{"nope"}}}

	err := a.applyStartupSwitches(t.Context())
	if err == nil {
		t.Fatal("applyStartupSwitches: want error for unknown job, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("applyStartupSwitches error = %q, want it to name %q", err.Error(), "nope")
	}
}

func TestShutdown_InFlightRequestContextSurvivesLifecycleCancel(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		result <- r.Context().Err()
	})
	a := &App{cfg: &config.Config{}}
	srv := httptest.NewUnstartedServer(slow)
	srv.Config.BaseContext = a.newServer(lifecycle, slow).BaseContext
	srv.Start()
	defer srv.Close()

	go func() {
		if resp, err := http.Get(srv.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("in-flight request context cancelled by lifecycle cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestApplyDisabledJobs_DisabledRankingRefreshNeverRefreshes(t *testing.T) {
	store := &countingBehavioralStore{}
	consumer := discovery.NewSatisfactionConsumer(store)
	searchSvc := discovery.NewSearchService(nil, nil, discovery.WithBehavioralRanking(consumer))
	a := &App{cfg: &config.Config{
		BehavioralRankingEnabled: true,
		DisabledJobs:             []string{"behavioral ranking refresh"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	if err := a.applyDisabledJobs(t.Context()); err != nil {
		t.Fatalf("applyDisabledJobs: %v", err)
	}
	a.startDiscoveryBackgroundJobs(ctx, &discovery.Discovery{Search: searchSvc})
	waitForHealth(t, a, jobBehavioralRankingRefresh, func(h runloop.JobHealth) bool { return h.Skipped >= 1 })

	if got := store.calls.Load(); got != 0 {
		t.Fatalf("RefreshBehavioralScores ran %d times, want 0 while disabled", got)
	}
}

func TestApplyStartupSwitches_ObserveLoopsAreNotSwitchable(t *testing.T) {
	for _, name := range []jobName{jobAlertMonitor, jobEvalMeter} {
		a := &App{cfg: &config.Config{DisabledJobs: []string{string(name)}}}
		if err := a.applyStartupSwitches(t.Context()); err == nil {
			t.Errorf("DISABLED_JOBS=%q accepted, want the unknown-job startup error", name)
		}
	}
}

type countingBehavioralStore struct{ calls atomic.Int64 }

func (s *countingBehavioralStore) SatisfactionSignals(context.Context, time.Time) ([]discovery.BehavioralSignal, error) {
	s.calls.Add(1)
	return nil, nil
}

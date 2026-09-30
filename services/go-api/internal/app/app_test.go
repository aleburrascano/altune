package app

import (
	catalogDomain "altune/go-api/internal/catalog/domain"
	catalogService "altune/go-api/internal/catalog/service"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingFiller struct {
	setCalls int
}

func (f *recordingFiller) SetTrackNumber(context.Context, catalogDomain.TrackId, shared.UserId, int) (bool, error) {
	f.setCalls++
	return true, nil
}

func (f *recordingFiller) GetByID(context.Context, catalogDomain.TrackId, shared.UserId) (*catalogDomain.Track, error) {
	return nil, nil
}

func TestCatalogTrackNumberSetter_SurfacesMalformedId(t *testing.T) {
	filler := &recordingFiller{}
	setter := catalogTrackNumberSetter{svc: catalogService.NewSetTrackNumberService(filler)}

	_, err := setter.Execute(context.Background(), shared.NewUserId(uuid.New()), "not-a-uuid", 3)

	if err == nil {
		t.Fatal("expected an error for a malformed track ID")
	}
	if filler.setCalls != 0 {
		t.Errorf("SetTrackNumber called %d times, want 0", filler.setCalls)
	}
}

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

	wired, err := a.wireCatalog(nil, discoveryWiring{})
	if err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if wired.scheduler == nil {
		t.Fatal("scheduler not wired, cannot assert pause")
	}
	a.scheduler = wired.scheduler

	if err := a.applyStartupSwitches(); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}

	if !wired.scheduler.Status().Paused {
		t.Fatal("Status().Paused = false, want true after ACQUISITION_PAUSED=true")
	}
}

func TestApplyStartupSwitches_AcquisitionPausedNoSchedulerIsNoop(t *testing.T) {
	a := &App{cfg: &config.Config{AcquisitionPaused: true}}

	if err := a.applyStartupSwitches(); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}
}

func TestApplyStartupSwitches_DisabledJobsDisableEachNamedJob(t *testing.T) {
	a := &App{cfg: &config.Config{DisabledJobs: []string{"eval meter", "stream recovery"}}}

	if err := a.applyStartupSwitches(); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}

	for _, name := range []string{"eval meter", "stream recovery"} {
		h := findJobHealth(t, a.JobHealth(), name)
		if h.Enabled {
			t.Errorf("job %q enabled, want disabled via DISABLED_JOBS", name)
		}
	}
}

func TestApplyStartupSwitches_DisabledJobsUnknownNameFailsStartup(t *testing.T) {
	a := &App{cfg: &config.Config{DisabledJobs: []string{"nope"}}}

	err := a.applyStartupSwitches()
	if err == nil {
		t.Fatal("applyStartupSwitches: want error for unknown job, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("applyStartupSwitches error = %q, want it to name %q", err.Error(), "nope")
	}
}

func TestApplyStartupSwitches_StreamripServices(t *testing.T) {
	tests := []struct {
		name     string
		services []string
		wantErr  string
	}{
		{"typo names variable and entry", []string{"qobuz", "tidall"}, "STREAMRIP_SERVICES"},
		{"typo entry is quoted", []string{"tidall"}, `"tidall"`},
		{"mixed case padded and empty accepted", []string{"Tidal", " qobuz ", ""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: &config.Config{StreamripServices: tt.services}}

			err := a.applyStartupSwitches()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("applyStartupSwitches: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("applyStartupSwitches error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
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
	consumer := discoveryService.NewSatisfactionConsumer(store)
	searchSvc := discoveryService.NewService(nil, nil, discoveryService.WithBehavioralRanking(consumer))
	a := &App{cfg: &config.Config{
		BehavioralRankingEnabled: true,
		DisabledJobs:             []string{"behavioral ranking refresh"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	if err := a.applyDisabledJobs(); err != nil {
		t.Fatalf("applyDisabledJobs: %v", err)
	}
	a.startDiscoveryBackgroundJobs(ctx, newClientFactory(nil), searchSvc, nil, nil)
	waitForHealth(t, a, jobBehavioralRankingRefresh, func(h JobHealth) bool { return h.Skipped >= 1 })

	if got := store.calls.Load(); got != 0 {
		t.Fatalf("RefreshBehavioralScores ran %d times, want 0 while disabled", got)
	}
}

func TestWhenLeaderUnlessDisabled_DisabledMonitorsNeverStart(t *testing.T) {
	for _, name := range []jobName{jobAlertMonitor, jobEvalMeter} {
		t.Run(string(name), func(t *testing.T) {
			a := &App{cfg: &config.Config{DisabledJobs: []string{string(name)}}}
			var started atomic.Bool
			a.whenLeaderUnlessDisabled(name, func(context.Context) { started.Store(true) })
			if err := a.applyDisabledJobs(); err != nil {
				t.Fatalf("applyDisabledJobs: %v", err)
			}

			for _, job := range a.backgroundStarts {
				job.start(context.Background())
			}

			if started.Load() {
				t.Fatalf("%s started despite DISABLED_JOBS", name)
			}
		})
	}
}

type countingBehavioralStore struct{ calls atomic.Int64 }

func (s *countingBehavioralStore) SatisfactionSignals(context.Context, time.Time) ([]discoveryPorts.BehavioralSignal, error) {
	s.calls.Add(1)
	return nil, nil
}

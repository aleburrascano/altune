package app

import (
	catalogDomain "altune/go-api/internal/catalog/domain"
	catalogService "altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestApplyStartupSwitches_AcquisitionPausedPausesWiredScheduler is the
// regression for #2800: ACQUISITION_PAUSED=true must pause the scheduler the
// moment it is wired, with no /admin POST needed, so Status().Paused already
// reports true at startup.
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

	if _, err := a.wireCatalog(nil, nil, nil); err != nil {
		t.Fatalf("wireCatalog: %v", err)
	}
	if a.scheduler == nil {
		t.Fatal("scheduler not wired, cannot assert pause")
	}

	if err := a.applyStartupSwitches(); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}

	if !a.scheduler.Status().Paused {
		t.Fatal("Status().Paused = false, want true after ACQUISITION_PAUSED=true")
	}
}

// TestApplyStartupSwitches_AcquisitionPausedNoSchedulerIsNoop confirms that with
// no scheduler wired (no audio store/source enabled), ACQUISITION_PAUSED is a
// no-op rather than a startup failure.
func TestApplyStartupSwitches_AcquisitionPausedNoSchedulerIsNoop(t *testing.T) {
	a := &App{cfg: &config.Config{AcquisitionPaused: true}}

	if err := a.applyStartupSwitches(); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}
}

// TestApplyStartupSwitches_DisabledJobsDisableEachNamedJob is the regression
// for #2800: DISABLED_JOBS lists jobs that must report disabled in JobHealth()
// at startup, without a runtime POST.
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

// TestApplyStartupSwitches_DisabledJobsUnknownNameFailsStartup is the
// regression for #2800: a mistyped DISABLED_JOBS entry must fail app
// construction with an error that names the offending job, rather than being
// silently ignored.
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

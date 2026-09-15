package app

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	catalogService "altune/go-api/internal/catalog/service"

	"github.com/google/uuid"
)

// flipNamedJob flips the kill switch of the job with the given wire name
// through the operator-gated admin router.
func flipNamedJob(t *testing.T, srv http.Handler, name jobName, action string) {
	t.Helper()
	path := "/admin/jobs/" + url.PathEscape(string(name)) + "/" + action
	if code, body := callAdmin(t, srv, http.MethodPost, path, operatorToken); code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200; body %s", path, code, body)
	}
}

type countingStalePendingFailer struct{ calls atomic.Int64 }

func (f *countingStalePendingFailer) FailStalePending(context.Context, time.Time, string) (int, error) {
	f.calls.Add(1)
	return 0, nil
}

// TestStalePendingReconcile_AdminKillSwitchSuppressesSweep is the regression for
// #1062 on the reconcile half: the production stale-pending job, disabled through
// the admin router, must not touch the repository on its leader-acquired run.
func TestStalePendingReconcile_AdminKillSwitchSuppressesSweep(t *testing.T) {
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	repo := &countingStalePendingFailer{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	a.startStalePendingReconcile(ctx, repo)
	flipNamedJob(t, srv, jobStalePendingReconcile, "disable")
	for _, job := range a.backgroundStarts {
		job.start(ctx) // runs the first tick synchronously on its goroutine
	}

	h := waitForHealth(t, a, jobStalePendingReconcile, func(h JobHealth) bool { return h.Skipped >= 1 })
	if h.Enabled {
		t.Fatalf("job still enabled after admin disable: %+v", h)
	}
	if got := repo.calls.Load(); got != 0 {
		t.Fatalf("disabled stale-pending reconcile swept the repo %d times", got)
	}
}

// TestStreamRecovery_AdminKillSwitchSuppressesReschedule is the regression for
// #1062 on the stream half: with "stream recovery" disabled through the admin
// router, a stream whose audio storage reports missing must neither mark the
// track failed nor schedule re-acquisition, and re-enabling restores recovery.
func TestStreamRecovery_AdminKillSwitchSuppressesReschedule(t *testing.T) {
	ctx := context.Background()
	a := &App{}
	srv := jobsAdminServer(t, a, true)
	user := shared.NewUserId(uuid.New())

	repo := catalogtest.NewTrackRepo()
	store := catalogtest.NewAudioStore()
	store.ErrOnStream = errors.New("not found")
	sched := &catalogtest.Scheduler{}
	svc := catalogService.NewStreamTrackService(repo, store,
		catalogService.WithStreamScheduler(sched),
		catalogService.WithStreamRecoverySwitch(a.jobSwitch(jobStreamRecovery)))

	track, err := domain.NewTrack(user, "Track", "Artist", "Album")
	if err != nil {
		t.Fatal(err)
	}
	repo.Seed(track)
	if err := track.MarkReady("audio/gone.opus"); err != nil {
		t.Fatal(err)
	}

	flipNamedJob(t, srv, jobStreamRecovery, "disable")

	if _, err := svc.Execute(ctx, user, track.ID); !errors.Is(err, catalogService.ErrAudioTemporarilyUnavailable) {
		t.Fatalf("disabled recovery stream err = %v, want ErrAudioTemporarilyUnavailable", err)
	}
	if err := svc.RecoverIfMissing(ctx, user, track.ID); err != nil {
		t.Fatalf("disabled RecoverIfMissing err = %v, want nil no-op", err)
	}
	if len(sched.TrackIds) != 0 {
		t.Fatalf("disabled stream recovery scheduled %d re-acquisitions", len(sched.TrackIds))
	}
	if got, _ := repo.GetByID(ctx, track.ID, user); got == nil || got.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("disabled stream recovery changed the track: %+v", got)
	}
	if h := findJobHealth(t, a.JobHealth(), string(jobStreamRecovery)); h.Enabled || h.Skipped != 2 {
		t.Fatalf("stream recovery health = %+v, want disabled with 2 skipped", h)
	}

	flipNamedJob(t, srv, jobStreamRecovery, "enable")

	if _, err := svc.Execute(ctx, user, track.ID); !errors.Is(err, catalogService.ErrAudioNotAvailable) {
		t.Fatalf("re-enabled recovery stream err = %v, want ErrAudioNotAvailable", err)
	}
	if len(sched.TrackIds) != 1 {
		t.Fatalf("re-enabled stream recovery scheduled %d re-acquisitions, want 1", len(sched.TrackIds))
	}
}

func waitForHealth(t *testing.T, a *App, name jobName, cond func(JobHealth) bool) JobHealth {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h := findJobHealth(t, a.JobHealth(), string(name))
		if cond(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %q stayed %+v", name, h)
		}
		time.Sleep(time.Millisecond)
	}
}

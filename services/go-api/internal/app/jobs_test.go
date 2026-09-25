package app

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	catalogService "altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestJobHealth_RecordsSuccessAndFailure is the regression for the missing
// health signal: each run must leave a queryable last-success/last-failure
// timestamp and a cumulative failure count per job.
func TestJobHealth_RecordsSuccessAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	a := &App{}
	// Odd calls succeed, even calls fail, so both signals accumulate.
	a.runTicker(ctx, "rollup", time.Millisecond, func(context.Context) error {
		if calls.Add(1)%2 == 0 {
			return errors.New("boom")
		}
		return nil
	})

	waitForAtLeast(t, &calls, 4)
	cancel()

	h := findJobHealth(t, a.JobHealth(), "rollup")
	if !h.Enabled {
		t.Error("job should report enabled when its kill switch is untouched")
	}
	if h.LastSuccess.IsZero() {
		t.Error("health signal did not record a last-success timestamp")
	}
	if h.Failures == 0 {
		t.Error("health signal did not count any failures")
	}
	if h.LastFailure.IsZero() {
		t.Error("health signal did not record a last-failure timestamp")
	}
}

// TestJobHealth_ReflectsKillSwitch confirms the queryable snapshot tracks the
// kill switch so an operator can see which jobs are currently suspended.
func TestJobHealth_ReflectsKillSwitch(t *testing.T) {
	a := &App{}
	a.job("paused")
	if _, ok := a.SetJobEnabled("paused", false); !ok {
		t.Fatal("SetJobEnabled on a registered job reported unknown")
	}

	h := findJobHealth(t, a.JobHealth(), "paused")
	if h.Enabled {
		t.Error("a disabled job must report Enabled=false in its health snapshot")
	}
}

func findJobHealth(t *testing.T, snapshot []JobHealth, name string) JobHealth {
	t.Helper()
	for _, h := range snapshot {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("job %q not found in health snapshot", name)
	return JobHealth{}
}

// TestSetJobEnabled_UnknownJobRegistersNothing guards the admin kill switch
// against a mistyped job name minting a phantom job in the health snapshot.
func TestSetJobEnabled_UnknownJobRegistersNothing(t *testing.T) {
	a := &App{}
	if _, ok := a.SetJobEnabled("typo", false); ok {
		t.Fatal("SetJobEnabled on an unregistered job reported ok")
	}
	if got := a.JobHealth(); len(got) != 0 {
		t.Fatalf("unknown job name registered a job: %+v", got)
	}
}

// TestJobNames_WireIdentifiersUnchanged pins every background job's name to the
// string operators use on GET /admin/jobs and POST /admin/jobs/{name}/enable|
// disable, and proves a job registered under its typed constant is addressable
// through the admin switchboard by that exact wire string.
func TestJobNames_WireIdentifiersUnchanged(t *testing.T) {
	want := map[jobName]string{
		jobEvalMeter:                "eval meter",
		jobAlertMonitor:             "alert monitor",
		jobStalePendingReconcile:    "stale pending reconcile",
		jobOrphanedAudioReconcile:   "orphaned audio reconcile",
		jobBehavioralCorpusRefresh:  "behavioral corpus refresh",
		jobDiscoveryMetricsRollup:   "discovery metrics rollup",
		jobVocabularyRefresh:        "vocabulary refresh",
		jobBehavioralRankingRefresh: "behavioral ranking refresh",
	}
	for name, wire := range want {
		a := &App{}
		a.startTicker(context.Background(), name, time.Hour, func(context.Context) error { return nil })
		st, ok := adminJobs{app: a}.SetJobEnabled(wire, false)
		if !ok {
			t.Fatalf("job %q not addressable by wire name %q", name, wire)
		}
		if st.Name != wire || st.Enabled {
			t.Fatalf("switchboard status = %+v, want name %q disabled", st, wire)
		}
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

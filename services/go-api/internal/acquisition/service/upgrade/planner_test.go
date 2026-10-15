package upgrade_test

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type pickingStore struct {
	upgradeStore
	picks     []ports.UpgradeCandidate
	pickErr   error
	selection ports.UpgradeSelection
}

func (s *pickingStore) Candidates(_ context.Context, selection ports.UpgradeSelection) ([]ports.UpgradeCandidate, error) {
	s.selection = selection
	return s.picks, s.pickErr
}

type recordingScheduler struct {
	scheduled []shared.TrackId
	err       error
}

func (r *recordingScheduler) ScheduleUpgrade(_ context.Context, _ shared.UserId, trackId shared.TrackId) error {
	r.scheduled = append(r.scheduled, trackId)
	return r.err
}

func TestPlanner_SchedulesThePickedTrackForTheCurrentGeneration(t *testing.T) {
	trackId := domain.NewTrackId()
	store := &pickingStore{picks: []ports.UpgradeCandidate{{TrackID: trackId, UserID: shared.NewUserId(uuid.New()), Grade: "F"}}}
	scheduler := &recordingScheduler{}

	if err := upgrade.NewPlanner(store, scheduler, 2, 6*time.Hour).PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	if len(scheduler.scheduled) != 1 || scheduler.scheduled[0] != trackId {
		t.Errorf("scheduled = %v, want the picked track", scheduler.scheduled)
	}
	want := ports.UpgradeSelection{Limit: 1, Generation: grade.PipelineGeneration, RetryAfter: 6 * time.Hour, MaxQueued: 2}
	if store.selection != want {
		t.Errorf("selection = %+v, want %+v", store.selection, want)
	}
}

func TestPlanner_CheckingUploadsAsksTheStoreForUnverifiedUserUploadsToo(t *testing.T) {
	store := &pickingStore{}

	if err := upgrade.NewPlanner(store, &recordingScheduler{}, 2, 6*time.Hour).CheckingUploads().PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	want := ports.UpgradeSelection{Limit: 1, Generation: grade.PipelineGeneration, RetryAfter: 6 * time.Hour, MaxQueued: 2, IncludeUnverifiedUploads: true}
	if store.selection != want {
		t.Errorf("selection = %+v, want %+v", store.selection, want)
	}
}

func TestPlanner_SchedulesNothingWhenNothingIsPicked(t *testing.T) {
	scheduler := &recordingScheduler{}

	if err := upgrade.NewPlanner(&pickingStore{}, scheduler, 1, time.Hour).PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	if len(scheduler.scheduled) != 0 {
		t.Errorf("scheduled = %v, want none", scheduler.scheduled)
	}
}

func TestPlanner_ReportsAFailedPickOrSchedule(t *testing.T) {
	pickFails := &pickingStore{pickErr: errors.New("db down")}
	if err := upgrade.NewPlanner(pickFails, &recordingScheduler{}, 1, time.Hour).PlanNext(context.Background()); !errors.Is(err, pickFails.pickErr) {
		t.Errorf("PlanNext with a failing pick = %v, want the pick error", err)
	}

	scheduleErr := errors.New("paused")
	picks := &pickingStore{picks: []ports.UpgradeCandidate{{TrackID: domain.NewTrackId()}}}
	if err := upgrade.NewPlanner(picks, &recordingScheduler{err: scheduleErr}, 1, time.Hour).PlanNext(context.Background()); !errors.Is(err, scheduleErr) {
		t.Errorf("PlanNext with a failing schedule = %v, want the schedule error", err)
	}
}

func TestPlanner_ASchedulerThatIsPausedIsNotAnError(t *testing.T) {
	picks := &pickingStore{picks: []ports.UpgradeCandidate{{TrackID: domain.NewTrackId()}}}
	scheduler := &recordingScheduler{err: acqdomain.ErrAcquisitionPaused}

	if err := upgrade.NewPlanner(picks, scheduler, 1, time.Hour).PlanNext(context.Background()); err != nil {
		t.Errorf("PlanNext while paused = %v, want nil: a paused lane is not a failing job", err)
	}
}

func TestInterval_SpreadsTheHourlyBudgetEvenly(t *testing.T) {
	cases := map[int]time.Duration{1: time.Hour, 30: 2 * time.Minute, 3600: time.Second, 0: time.Hour}
	for perHour, want := range cases {
		if got := upgrade.Interval(perHour); got != want {
			t.Errorf("Interval(%d) = %s, want %s", perHour, got, want)
		}
	}
}

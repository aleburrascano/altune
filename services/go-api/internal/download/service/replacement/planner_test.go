package replacement_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type pickingStore struct {
	upgradeStore
	picks     []download.UpgradeCandidate
	pickErr   error
	selection download.UpgradeSelection
}

func (s *pickingStore) Candidates(_ context.Context, selection download.UpgradeSelection) ([]download.UpgradeCandidate, error) {
	s.selection = selection
	return s.picks, s.pickErr
}

type recordingScheduler struct {
	scheduled []ids.TrackId
	err       error
}

func (r *recordingScheduler) ScheduleUpgrade(_ context.Context, _ ids.UserId, trackId ids.TrackId) error {
	r.scheduled = append(r.scheduled, trackId)
	return r.err
}

func TestPlanner_SchedulesThePickedTrackForTheCurrentGeneration(t *testing.T) {
	trackId := ids.NewTrackId()
	store := &pickingStore{picks: []download.UpgradeCandidate{{TrackID: trackId, UserID: ids.NewUserId(uuid.New()), Grade: "F"}}}
	scheduler := &recordingScheduler{}

	if err := download.NewReplacementPlanner(store, scheduler, 2, 6*time.Hour).PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	if len(scheduler.scheduled) != 1 || scheduler.scheduled[0] != trackId {
		t.Errorf("scheduled = %v, want the picked track", scheduler.scheduled)
	}
	want := download.UpgradeSelection{Limit: 1, Generation: download.PipelineGeneration, RetryAfter: 6 * time.Hour, MaxQueued: 2}
	if store.selection != want {
		t.Errorf("selection = %+v, want %+v", store.selection, want)
	}
}

func TestPlanner_CheckingUploadsAsksTheStoreForUnverifiedUserUploadsToo(t *testing.T) {
	store := &pickingStore{}

	if err := download.NewReplacementPlanner(store, &recordingScheduler{}, 2, 6*time.Hour).CheckingUploads().PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	want := download.UpgradeSelection{Limit: 1, Generation: download.PipelineGeneration, RetryAfter: 6 * time.Hour, MaxQueued: 2, CheckInPlace: true}
	if store.selection != want {
		t.Errorf("selection = %+v, want %+v", store.selection, want)
	}
}

func TestPlanner_SchedulesNothingWhenNothingIsPicked(t *testing.T) {
	scheduler := &recordingScheduler{}

	if err := download.NewReplacementPlanner(&pickingStore{}, scheduler, 1, time.Hour).PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	if len(scheduler.scheduled) != 0 {
		t.Errorf("scheduled = %v, want none", scheduler.scheduled)
	}
}

func TestPlanner_ReportsAFailedPickOrSchedule(t *testing.T) {
	pickFails := &pickingStore{pickErr: errors.New("db down")}
	if err := download.NewReplacementPlanner(pickFails, &recordingScheduler{}, 1, time.Hour).PlanNext(context.Background()); !errors.Is(err, pickFails.pickErr) {
		t.Errorf("PlanNext with a failing pick = %v, want the pick error", err)
	}

	scheduleErr := errors.New("paused")
	picks := &pickingStore{picks: []download.UpgradeCandidate{{TrackID: ids.NewTrackId()}}}
	if err := download.NewReplacementPlanner(picks, &recordingScheduler{err: scheduleErr}, 1, time.Hour).PlanNext(context.Background()); !errors.Is(err, scheduleErr) {
		t.Errorf("PlanNext with a failing schedule = %v, want the schedule error", err)
	}
}

func TestPlanner_ASchedulerThatIsPausedIsNotAnError(t *testing.T) {
	picks := &pickingStore{picks: []download.UpgradeCandidate{{TrackID: ids.NewTrackId()}}}
	scheduler := &recordingScheduler{err: download.ErrAcquisitionPaused}

	if err := download.NewReplacementPlanner(picks, scheduler, 1, time.Hour).PlanNext(context.Background()); err != nil {
		t.Errorf("PlanNext while paused = %v, want nil: a paused lane is not a failing job", err)
	}
}

func TestInterval_SpreadsTheHourlyBudgetEvenly(t *testing.T) {
	cases := map[int]time.Duration{1: time.Hour, 30: 2 * time.Minute, 3600: time.Second, 0: time.Hour}
	for perHour, want := range cases {
		if got := download.UpgradeInterval(perHour); got != want {
			t.Errorf("Interval(%d) = %s, want %s", perHour, got, want)
		}
	}
}

func TestPlanner_ATrackWithAJobInFlightIsSkippedNotAFailure(t *testing.T) {
	picks := &pickingStore{picks: []download.UpgradeCandidate{{TrackID: ids.NewTrackId()}}}
	scheduler := &recordingScheduler{err: download.ErrTrackJobInFlight}

	if err := download.NewReplacementPlanner(picks, scheduler, 1, time.Hour).PlanNext(context.Background()); err != nil {
		t.Errorf("PlanNext with a job in flight = %v, want nil: the batch goes on", err)
	}
}

func TestPlanner_RescuingLowBitrateAsksTheStoreForTheFloor(t *testing.T) {
	store := &pickingStore{}

	if err := download.NewReplacementPlanner(store, &recordingScheduler{}, 2, 6*time.Hour).RescuingLowBitrate(112).PlanNext(context.Background()); err != nil {
		t.Fatalf("PlanNext: %v", err)
	}

	want := download.UpgradeSelection{Limit: 1, Generation: download.PipelineGeneration, RetryAfter: 6 * time.Hour, MaxQueued: 2, LowBitrateFloorKbps: 112}
	if store.selection != want {
		t.Errorf("selection = %+v, want %+v", store.selection, want)
	}
}

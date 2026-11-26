package service_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func blindingLights() *download.TrackSnapshot {
	return &download.TrackSnapshot{
		ID:     ids.NewTrackId(),
		UserId: ids.NewUserId(uuid.New()),
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
	}
}

func failedTrack() *download.TrackSnapshot {
	track := blindingLights()
	track.Status = download.TrackFailed
	return track
}

func readyTrack() *download.TrackSnapshot {
	track := blindingLights()
	track.Status, track.Streamable = download.TrackReady, true
	return track
}

func pendingTrack() *download.TrackSnapshot {
	track := blindingLights()
	track.Status = download.TrackPending
	return track
}

func scheduleQueued() error { return nil }

var errNotQueued = errors.New("not queued")

func scheduleRefused() error { return errNotQueued }

type scheduleSpy struct{ calls int }

func (s *scheduleSpy) schedule() error {
	s.calls++
	return nil
}

func TestRetryAdmission_AdmitsTheFirstRetryOfAFailedTrack(t *testing.T) {
	spy := &scheduleSpy{}

	err := download.NewRetryAdmission(acquisitiontest.MustNewCooldownStore(time.Now)).Admit(context.Background(), failedTrack(), spy.schedule)

	if err != nil || spy.calls != 1 {
		t.Fatalf("Admit = %v with %d schedules, want nil and one schedule", err, spy.calls)
	}
}

func TestRetryAdmission_RefusesASecondRetryInsideTheCooldownWithoutScheduling(t *testing.T) {
	admission := download.NewRetryAdmission(acquisitiontest.MustNewCooldownStore(time.Now))
	track := failedTrack()
	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	spy := &scheduleSpy{}

	err := admission.Admit(context.Background(), track, spy.schedule)

	if !errors.Is(err, download.ErrCooldownActive) {
		t.Errorf("second Admit = %v, want ErrCooldownActive", err)
	}
	if spy.calls != 0 {
		t.Error("schedule ran for a retry the cooldown refused")
	}
}

func TestRetryAdmission_AdmitsAgainOnceTheOneMinuteCooldownElapsed(t *testing.T) {
	manual := clocktest.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	store := acquisitiontest.MustNewCooldownStore(manual.Func())
	admission := download.NewRetryAdmission(store)
	track := failedTrack()
	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	manual.Advance(time.Minute + time.Second)

	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("Admit after the cooldown elapsed = %v, want nil", err)
	}
}

func TestRetryAdmission_AdmitsDistinctTracksIndependently(t *testing.T) {
	admission := download.NewRetryAdmission(acquisitiontest.MustNewCooldownStore(time.Now))

	if err := admission.Admit(context.Background(), failedTrack(), scheduleQueued); err != nil {
		t.Errorf("first track Admit = %v, want nil", err)
	}
	if err := admission.Admit(context.Background(), failedTrack(), scheduleQueued); err != nil {
		t.Errorf("second track Admit = %v, want nil", err)
	}
}

func TestRetryAdmission_RefusesATrackThatHasNotFailed(t *testing.T) {
	cases := map[string]*download.TrackSnapshot{"pending": pendingTrack(), "ready": readyTrack(), "nil": nil}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			spy := &scheduleSpy{}

			err := download.NewRetryAdmission(acquisitiontest.MustNewCooldownStore(time.Now)).Admit(context.Background(), track, spy.schedule)

			if !errors.Is(err, download.ErrRetryNotFailed) || spy.calls != 0 {
				t.Errorf("Admit = %v with %d schedules, want ErrRetryNotFailed and none", err, spy.calls)
			}
		})
	}
}

func TestReacquireAdmission_RefusesATrackThatIsNotStreamable(t *testing.T) {
	cases := map[string]*download.TrackSnapshot{"pending": pendingTrack(), "failed": failedTrack(), "nil": nil}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			spy := &scheduleSpy{}

			err := download.NewReacquireAdmission(acquisitiontest.MustNewCooldownStore(time.Now)).Admit(context.Background(), track, spy.schedule)

			if !errors.Is(err, download.ErrReacquireNotReady) || spy.calls != 0 {
				t.Errorf("Admit = %v with %d schedules, want ErrReacquireNotReady and none", err, spy.calls)
			}
		})
	}
}

func TestReacquireAdmission_AdmitsAStreamableTrackOnceThenHoldsTheCooldown(t *testing.T) {
	admission := download.NewReacquireAdmission(acquisitiontest.MustNewCooldownStore(time.Now))
	track := readyTrack()

	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	if err := admission.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, download.ErrCooldownActive) {
		t.Errorf("second Admit = %v, want ErrCooldownActive", err)
	}
}

func TestAdmission_RetryAndReacquireWindowsAreIndependent(t *testing.T) {
	store := acquisitiontest.MustNewCooldownStore(time.Now)
	track := failedTrack()
	if err := download.NewRetryAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("retry Admit = %v, want nil", err)
	}
	track.Status, track.Streamable = download.TrackReady, true

	if err := download.NewReacquireAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("reacquire Admit after retry = %v, want nil (separate kinds)", err)
	}
}

func TestAdmission_TheCooldownLivesInTheStoreAndSurvivesARestart(t *testing.T) {
	store := acquisitiontest.MustNewCooldownStore(time.Now)
	retryTrack, reacquireTrack := failedTrack(), readyTrack()
	if err := download.NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); err != nil {
		t.Fatalf("first retry Admit = %v, want nil", err)
	}
	if err := download.NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); err != nil {
		t.Fatalf("first reacquire Admit = %v, want nil", err)
	}

	if err := download.NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); !errors.Is(err, download.ErrCooldownActive) {
		t.Errorf("retry Admit after restart = %v, want ErrCooldownActive", err)
	}
	if err := download.NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); !errors.Is(err, download.ErrCooldownActive) {
		t.Errorf("reacquire Admit after restart = %v, want ErrCooldownActive", err)
	}
}

func TestRetryAdmission_AStoreErrorRefusesWithoutSchedulingAndKeepsTheCause(t *testing.T) {
	store := acquisitiontest.MustNewCooldownStore(time.Now)
	reserveErr := fmt.Errorf("reserve cooldown: %w", &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "acquisition_cooldowns" does not exist`,
	})
	store.FailReserve(reserveErr)
	spy := &scheduleSpy{}

	err := download.NewRetryAdmission(store).Admit(context.Background(), failedTrack(), spy.schedule)

	var pgErr *pgconn.PgError
	if !errors.Is(err, reserveErr) || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("Admit = %v, want the store error carrying SQLSTATE 42P01", err)
	}
	if spy.calls != 0 {
		t.Error("retry was scheduled although the cooldown store failed")
	}
}

func TestRetryAdmission_ARefusedScheduleRefundsTheCooldownAfterTheCallerHasGone(t *testing.T) {
	admission := download.NewRetryAdmission(acquisitiontest.MustNewCooldownStore(time.Now))
	track := failedTrack()
	gone, cancel := context.WithCancel(context.Background())
	cancel()

	if err := admission.Admit(gone, track, scheduleRefused); !errors.Is(err, errNotQueued) {
		t.Fatalf("Admit with a refused schedule = %v, want the schedule error", err)
	}
	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("Admit after the refused schedule = %v, want nil (cooldown refunded)", err)
	}
}

func TestRetryAdmission_AFailedRefundStillReturnsTheScheduleError(t *testing.T) {
	store := acquisitiontest.MustNewCooldownStore(time.Now)
	releaseErr := errors.New("db down")
	store.FailRelease(releaseErr)

	err := download.NewRetryAdmission(store).Admit(context.Background(), failedTrack(), scheduleRefused)

	if !errors.Is(err, errNotQueued) || errors.Is(err, releaseErr) {
		t.Errorf("Admit = %v, want the schedule error, not the release error", err)
	}
	if got := store.Releases(); got != 1 {
		t.Errorf("release attempts = %d, want 1", got)
	}
}

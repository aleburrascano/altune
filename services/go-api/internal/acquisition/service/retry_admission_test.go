package service_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type cooldownStore struct {
	mu         sync.Mutex
	now        func() time.Time
	lastAt     map[string]time.Time
	reserveErr error
	releaseErr error
	released   int
}

func newCooldownStore() *cooldownStore {
	return &cooldownStore{now: time.Now, lastAt: make(map[string]time.Time)}
}

func cooldownKey(trackID shared.TrackId, kind acquisition.CooldownKind) string {
	return string(kind) + "/" + trackID.String()
}

func (s *cooldownStore) Reserve(_ context.Context, trackID shared.TrackId, kind acquisition.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserveErr != nil {
		return time.Time{}, false, 0, s.reserveErr
	}
	now, key := s.now(), cooldownKey(trackID, kind)
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, cooldown - now.Sub(last), nil
	}
	s.lastAt[key] = now
	return now, true, 0, nil
}

func (s *cooldownStore) Release(ctx context.Context, trackID shared.TrackId, kind acquisition.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released++
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.releaseErr != nil {
		return s.releaseErr
	}
	key := cooldownKey(trackID, kind)
	if last, ok := s.lastAt[key]; ok && last.Equal(at) {
		delete(s.lastAt, key)
	}
	return nil
}

func blindingLights() *acquisition.TrackSnapshot {
	return &acquisition.TrackSnapshot{
		ID:     shared.NewTrackId(),
		UserId: shared.NewUserId(uuid.New()),
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
	}
}

func failedTrack() *acquisition.TrackSnapshot {
	track := blindingLights()
	track.Status = acquisition.TrackFailed
	return track
}

func readyTrack() *acquisition.TrackSnapshot {
	track := blindingLights()
	track.Status, track.Streamable = acquisition.TrackReady, true
	return track
}

func pendingTrack() *acquisition.TrackSnapshot {
	track := blindingLights()
	track.Status = acquisition.TrackPending
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

	err := acquisition.NewRetryAdmission(newCooldownStore()).Admit(context.Background(), failedTrack(), spy.schedule)

	if err != nil || spy.calls != 1 {
		t.Fatalf("Admit = %v with %d schedules, want nil and one schedule", err, spy.calls)
	}
}

func TestRetryAdmission_RefusesASecondRetryInsideTheCooldownWithoutScheduling(t *testing.T) {
	admission := acquisition.NewRetryAdmission(newCooldownStore())
	track := failedTrack()
	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	spy := &scheduleSpy{}

	err := admission.Admit(context.Background(), track, spy.schedule)

	if !errors.Is(err, acquisition.ErrCooldownActive) {
		t.Errorf("second Admit = %v, want ErrCooldownActive", err)
	}
	if spy.calls != 0 {
		t.Error("schedule ran for a retry the cooldown refused")
	}
}

func TestRetryAdmission_AdmitsAgainOnceTheOneMinuteCooldownElapsed(t *testing.T) {
	store := newCooldownStore()
	admission := acquisition.NewRetryAdmission(store)
	track := failedTrack()
	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	store.now = func() time.Time { return time.Now().Add(time.Minute + time.Second) }

	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("Admit after the cooldown elapsed = %v, want nil", err)
	}
}

func TestRetryAdmission_AdmitsDistinctTracksIndependently(t *testing.T) {
	admission := acquisition.NewRetryAdmission(newCooldownStore())

	if err := admission.Admit(context.Background(), failedTrack(), scheduleQueued); err != nil {
		t.Errorf("first track Admit = %v, want nil", err)
	}
	if err := admission.Admit(context.Background(), failedTrack(), scheduleQueued); err != nil {
		t.Errorf("second track Admit = %v, want nil", err)
	}
}

func TestRetryAdmission_RefusesATrackThatHasNotFailed(t *testing.T) {
	cases := map[string]*acquisition.TrackSnapshot{"pending": pendingTrack(), "ready": readyTrack(), "nil": nil}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			spy := &scheduleSpy{}

			err := acquisition.NewRetryAdmission(newCooldownStore()).Admit(context.Background(), track, spy.schedule)

			if !errors.Is(err, acquisition.ErrRetryNotFailed) || spy.calls != 0 {
				t.Errorf("Admit = %v with %d schedules, want ErrRetryNotFailed and none", err, spy.calls)
			}
		})
	}
}

func TestReacquireAdmission_RefusesATrackThatIsNotStreamable(t *testing.T) {
	cases := map[string]*acquisition.TrackSnapshot{"pending": pendingTrack(), "failed": failedTrack(), "nil": nil}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			spy := &scheduleSpy{}

			err := acquisition.NewReacquireAdmission(newCooldownStore()).Admit(context.Background(), track, spy.schedule)

			if !errors.Is(err, acquisition.ErrReacquireNotReady) || spy.calls != 0 {
				t.Errorf("Admit = %v with %d schedules, want ErrReacquireNotReady and none", err, spy.calls)
			}
		})
	}
}

func TestReacquireAdmission_AdmitsAStreamableTrackOnceThenHoldsTheCooldown(t *testing.T) {
	admission := acquisition.NewReacquireAdmission(newCooldownStore())
	track := readyTrack()

	if err := admission.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	if err := admission.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, acquisition.ErrCooldownActive) {
		t.Errorf("second Admit = %v, want ErrCooldownActive", err)
	}
}

func TestAdmission_RetryAndReacquireWindowsAreIndependent(t *testing.T) {
	store := newCooldownStore()
	track := failedTrack()
	if err := acquisition.NewRetryAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("retry Admit = %v, want nil", err)
	}
	track.Status, track.Streamable = acquisition.TrackReady, true

	if err := acquisition.NewReacquireAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("reacquire Admit after retry = %v, want nil (separate kinds)", err)
	}
}

func TestAdmission_TheCooldownLivesInTheStoreAndSurvivesARestart(t *testing.T) {
	store := newCooldownStore()
	retryTrack, reacquireTrack := failedTrack(), readyTrack()
	if err := acquisition.NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); err != nil {
		t.Fatalf("first retry Admit = %v, want nil", err)
	}
	if err := acquisition.NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); err != nil {
		t.Fatalf("first reacquire Admit = %v, want nil", err)
	}

	if err := acquisition.NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); !errors.Is(err, acquisition.ErrCooldownActive) {
		t.Errorf("retry Admit after restart = %v, want ErrCooldownActive", err)
	}
	if err := acquisition.NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); !errors.Is(err, acquisition.ErrCooldownActive) {
		t.Errorf("reacquire Admit after restart = %v, want ErrCooldownActive", err)
	}
}

func TestRetryAdmission_AStoreErrorRefusesWithoutSchedulingAndKeepsTheCause(t *testing.T) {
	store := newCooldownStore()
	store.reserveErr = fmt.Errorf("reserve cooldown: %w", &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "acquisition_cooldowns" does not exist`,
	})
	spy := &scheduleSpy{}

	err := acquisition.NewRetryAdmission(store).Admit(context.Background(), failedTrack(), spy.schedule)

	var pgErr *pgconn.PgError
	if !errors.Is(err, store.reserveErr) || !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("Admit = %v, want the store error carrying SQLSTATE 42P01", err)
	}
	if spy.calls != 0 {
		t.Error("retry was scheduled although the cooldown store failed")
	}
}

func TestRetryAdmission_ARefusedScheduleRefundsTheCooldownAfterTheCallerHasGone(t *testing.T) {
	admission := acquisition.NewRetryAdmission(newCooldownStore())
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
	store := newCooldownStore()
	store.releaseErr = errors.New("db down")

	err := acquisition.NewRetryAdmission(store).Admit(context.Background(), failedTrack(), scheduleRefused)

	if !errors.Is(err, errNotQueued) || errors.Is(err, store.releaseErr) {
		t.Errorf("Admit = %v, want the schedule error, not the release error", err)
	}
	if store.released != 1 {
		t.Errorf("release attempts = %d, want 1", store.released)
	}
}

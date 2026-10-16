package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func snapshotWith(status ports.TrackStatus, streamable bool) *ports.TrackSnapshot {
	return &ports.TrackSnapshot{
		ID:         shared.NewTrackId(),
		UserId:     shared.NewUserId(uuid.New()),
		Title:      "Blinding Lights",
		Artist:     "The Weeknd",
		Album:      "After Hours",
		Status:     status,
		Streamable: streamable,
	}
}

func failedTrack(_ *testing.T) *ports.TrackSnapshot {
	return snapshotWith(ports.TrackFailed, false)
}

func readyTrack(_ *testing.T) *ports.TrackSnapshot {
	return snapshotWith(ports.TrackReady, true)
}

func pendingTrackOnly(_ *testing.T) *ports.TrackSnapshot {
	return snapshotWith(ports.TrackPending, false)
}

type fakeCooldownStore struct {
	mu         sync.Mutex
	now        func() time.Time
	lastAt     map[string]time.Time
	reserveErr error
	releaseErr error
	released   int
}

func newFakeCooldownStore() *fakeCooldownStore {
	return &fakeCooldownStore{now: time.Now, lastAt: make(map[string]time.Time)}
}

func cooldownKey(trackID shared.TrackId, kind ports.CooldownKind) string {
	return string(kind) + "/" + trackID.String()
}

func (s *fakeCooldownStore) Reserve(_ context.Context, trackID shared.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
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

func (s *fakeCooldownStore) Release(_ context.Context, trackID shared.TrackId, kind ports.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released++
	if s.releaseErr != nil {
		return s.releaseErr
	}
	key := cooldownKey(trackID, kind)
	if last, ok := s.lastAt[key]; ok && last.Equal(at) {
		delete(s.lastAt, key)
	}
	return nil
}

func scheduleRefused() error { return errors.New("not queued") }

func TestCooldownGate_AdmitsFirstCall(t *testing.T) {
	a := NewRetryAdmission(newFakeCooldownStore())
	if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
}

func TestCooldownGate_AdmitsAfterCooldownElapsed(t *testing.T) {
	store := newFakeCooldownStore()
	a := NewRetryAdmission(store)
	track := failedTrack(t)
	if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	store.now = func() time.Time { return time.Now().Add(RetryCooldown + time.Second) }
	if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("Admit after cooldown elapsed = %v, want nil", err)
	}
}

func TestCooldownGate_RetryAndReacquireWindowsAreIndependent(t *testing.T) {
	store := newFakeCooldownStore()
	track := failedTrack(t)
	if err := NewRetryAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Fatalf("retry Admit = %v, want nil", err)
	}
	track.Status, track.Streamable = ports.TrackReady, true
	if err := NewReacquireAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("reacquire Admit after retry = %v, want nil (separate kinds)", err)
	}
}

func TestAdmission_CooldownSurvivesRestart(t *testing.T) {
	store := newFakeCooldownStore()
	retryTrack, reacquireTrack := failedTrack(t), readyTrack(t)
	if err := NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); err != nil {
		t.Fatalf("first retry Admit = %v, want nil", err)
	}
	if err := NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); err != nil {
		t.Fatalf("first reacquire Admit = %v, want nil", err)
	}
	if err := NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
		t.Errorf("retry Admit after restart = %v, want ErrCooldownActive", err)
	}
	if err := NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
		t.Errorf("reacquire Admit after restart = %v, want ErrCooldownActive", err)
	}
}

func TestCooldownGate_StoreErrorSkipsSchedule(t *testing.T) {
	store := newFakeCooldownStore()
	store.reserveErr = errors.New("db down")
	called := false
	err := NewRetryAdmission(store).Admit(context.Background(), failedTrack(t), func() error { called = true; return nil })
	if !errors.Is(err, store.reserveErr) {
		t.Errorf("Admit = %v, want wrapped store error", err)
	}
	if called {
		t.Error("schedule ran although the cooldown could not be reserved")
	}
}

func TestCooldownGate_ReleaseOnRefusedScheduleSurvivesCancelledContext(t *testing.T) {
	store := newFakeCooldownStore()
	a := NewRetryAdmission(store)
	track := failedTrack(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Admit(ctx, track, scheduleRefused); err == nil {
		t.Fatal("Admit with refused schedule = nil, want the schedule error")
	}
	if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("Admit after refused schedule = %v, want nil (cooldown refunded)", err)
	}
}

func TestCooldownGate_ReleaseFailureReturnsScheduleError(t *testing.T) {
	store := newFakeCooldownStore()
	store.releaseErr = errors.New("db down")
	err := NewRetryAdmission(store).Admit(context.Background(), failedTrack(t), scheduleRefused)
	if err == nil || errors.Is(err, store.releaseErr) {
		t.Errorf("Admit = %v, want the schedule error, not the release error", err)
	}
	if store.released != 1 {
		t.Errorf("release attempts = %d, want 1", store.released)
	}
}

func TestRetryAdmission_Admit(t *testing.T) {
	t.Run("not failed yields ErrRetryNotFailed", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), pendingTrackOnly(t), scheduleQueued); !errors.Is(err, acqdomain.ErrRetryNotFailed) {
			t.Errorf("Admit = %v, want ErrRetryNotFailed", err)
		}
	})

	t.Run("ready is not failed", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), readyTrack(t), scheduleQueued); !errors.Is(err, acqdomain.ErrRetryNotFailed) {
			t.Errorf("Admit = %v, want ErrRetryNotFailed", err)
		}
	})

	t.Run("first failed retry admitted", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); err != nil {
			t.Errorf("Admit = %v, want nil", err)
		}
	})

	t.Run("second failed retry within cooldown yields ErrCooldownActive", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		track := failedTrack(t)
		if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
			t.Fatalf("first Admit = %v, want nil", err)
		}
		if err := a.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
			t.Errorf("second Admit = %v, want ErrCooldownActive", err)
		}
	})

	t.Run("distinct tracks admitted independently", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); err != nil {
			t.Errorf("first track Admit = %v, want nil", err)
		}
		if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); err != nil {
			t.Errorf("second track Admit = %v, want nil", err)
		}
	})
}

func TestReacquireAdmission_Admit(t *testing.T) {
	t.Run("not streamable yields ErrReacquireNotReady", func(t *testing.T) {
		a := NewReacquireAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), pendingTrackOnly(t), scheduleQueued); !errors.Is(err, acqdomain.ErrReacquireNotReady) {
			t.Errorf("Admit = %v, want ErrReacquireNotReady", err)
		}
	})

	t.Run("failed track is not streamable", func(t *testing.T) {
		a := NewReacquireAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); !errors.Is(err, acqdomain.ErrReacquireNotReady) {
			t.Errorf("Admit = %v, want ErrReacquireNotReady", err)
		}
	})

	t.Run("first streamable reacquire admitted", func(t *testing.T) {
		a := NewReacquireAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), readyTrack(t), scheduleQueued); err != nil {
			t.Errorf("Admit = %v, want nil", err)
		}
	})

	t.Run("second reacquire within cooldown yields ErrCooldownActive", func(t *testing.T) {
		a := NewReacquireAdmission(newFakeCooldownStore())
		track := readyTrack(t)
		if err := a.Admit(context.Background(), track, scheduleQueued); err != nil {
			t.Fatalf("first Admit = %v, want nil", err)
		}
		if err := a.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
			t.Errorf("second Admit = %v, want ErrCooldownActive", err)
		}
	})
}

func TestReacquireAdmission(t *testing.T) {
	if err := NewReacquireAdmission(newFakeCooldownStore()).Admit(context.Background(), pendingTrackOnly(t), scheduleQueued); !errors.Is(err, acqdomain.ErrReacquireNotReady) {
		t.Errorf("pending track: err = %v, want ErrReacquireNotReady", err)
	}

	if err := NewReacquireAdmission(newFakeCooldownStore()).Admit(context.Background(), failedTrack(t), scheduleQueued); !errors.Is(err, acqdomain.ErrReacquireNotReady) {
		t.Errorf("failed track: err = %v, want ErrReacquireNotReady", err)
	}

	ready := readyTrack(t)
	admission := NewReacquireAdmission(newFakeCooldownStore())
	if err := admission.Admit(context.Background(), ready, scheduleQueued); err != nil {
		t.Fatalf("ready track: err = %v, want admitted", err)
	}
	if err := admission.Admit(context.Background(), ready, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
		t.Errorf("second call: err = %v, want ErrCooldownActive", err)
	}
}

func TestRetryAdmission_StillFailedOnlyWithCooldown(t *testing.T) {
	if err := NewRetryAdmission(newFakeCooldownStore()).Admit(context.Background(), readyTrack(t), scheduleQueued); !errors.Is(err, acqdomain.ErrRetryNotFailed) {
		t.Errorf("ready track: err = %v, want ErrRetryNotFailed", err)
	}

	failed := failedTrack(t)
	admission := NewRetryAdmission(newFakeCooldownStore())
	if err := admission.Admit(context.Background(), failed, scheduleQueued); err != nil {
		t.Fatalf("failed track: err = %v, want admitted", err)
	}
	if err := admission.Admit(context.Background(), failed, scheduleQueued); !errors.Is(err, acqdomain.ErrCooldownActive) {
		t.Errorf("second call: err = %v, want ErrCooldownActive", err)
	}
}

func TestRetryAdmission_NilSnapshotRefused(t *testing.T) {
	scheduled := false
	err := NewRetryAdmission(newFakeCooldownStore()).Admit(context.Background(), nil, func() error { scheduled = true; return nil })
	if !errors.Is(err, acqdomain.ErrRetryNotFailed) {
		t.Errorf("err = %v, want ErrRetryNotFailed", err)
	}
	if scheduled {
		t.Error("scheduled a retry for a nil snapshot")
	}
}

func TestReacquireAdmission_NilSnapshotRefused(t *testing.T) {
	scheduled := false
	err := NewReacquireAdmission(newFakeCooldownStore()).Admit(context.Background(), nil, func() error { scheduled = true; return nil })
	if !errors.Is(err, acqdomain.ErrReacquireNotReady) {
		t.Errorf("err = %v, want ErrReacquireNotReady", err)
	}
	if scheduled {
		t.Error("scheduled a reacquire for a nil snapshot")
	}
}

func TestRetryAdmission_ACooldownStoreErrorRefusesTheRetry(t *testing.T) {
	store := newFakeCooldownStore()
	store.reserveErr = fmt.Errorf("reserve cooldown: %w", &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "acquisition_cooldowns" does not exist`,
	})
	called := false
	err := NewRetryAdmission(store).Admit(context.Background(), failedTrack(t), func() error { called = true; return nil })
	if !errors.Is(err, store.reserveErr) {
		t.Fatalf("Admit = %v, want the store error", err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		t.Errorf("Admit error = %v, want it to carry SQLSTATE 42P01", err)
	}
	if called {
		t.Error("retry was scheduled although the cooldown store failed")
	}
}

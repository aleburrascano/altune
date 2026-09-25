package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func failedTrack(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkFailed("yt-dlp exited 1"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	return track
}

func readyTrack(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady("audio-ref"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	return track
}

func pendingTrackOnly(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	return track
}

// fakeCooldownStore is an in-memory ports.CooldownStore with an injectable
// clock. Sharing one instance across admissions models the shared database
// that every process sees.
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

func cooldownKey(trackID domain.TrackId, kind ports.CooldownKind) string {
	return string(kind) + "/" + trackID.String()
}

func (s *fakeCooldownStore) Reserve(_ context.Context, trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserveErr != nil {
		return time.Time{}, false, s.reserveErr
	}
	now, key := s.now(), cooldownKey(trackID, kind)
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, nil
	}
	s.lastAt[key] = now
	return now, true, nil
}

func (s *fakeCooldownStore) Release(_ context.Context, trackID domain.TrackId, kind ports.CooldownKind, at time.Time) error {
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
	_ = track.MarkReady("audio-ref")
	if err := NewReacquireAdmission(store).Admit(context.Background(), track, scheduleQueued); err != nil {
		t.Errorf("reacquire Admit after retry = %v, want nil (separate kinds)", err)
	}
}

// TestAdmission_CooldownSurvivesRestart is the #986 regression: re-creating the
// admission, as a restarted process or a second replica does, must not reopen
// the window. Before the fix the window lived in a per-admission map, so the
// second admission was granted.
func TestAdmission_CooldownSurvivesRestart(t *testing.T) {
	store := newFakeCooldownStore()
	retryTrack, reacquireTrack := failedTrack(t), readyTrack(t)
	if err := NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); err != nil {
		t.Fatalf("first retry Admit = %v, want nil", err)
	}
	if err := NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); err != nil {
		t.Fatalf("first reacquire Admit = %v, want nil", err)
	}
	if err := NewRetryAdmission(store).Admit(context.Background(), retryTrack, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
		t.Errorf("retry Admit after restart = %v, want ErrCooldownActive", err)
	}
	if err := NewReacquireAdmission(store).Admit(context.Background(), reacquireTrack, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
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
		if err := a.Admit(context.Background(), pendingTrackOnly(t), scheduleQueued); !errors.Is(err, ErrRetryNotFailed) {
			t.Errorf("Admit = %v, want ErrRetryNotFailed", err)
		}
	})

	t.Run("ready is not failed", func(t *testing.T) {
		a := NewRetryAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), readyTrack(t), scheduleQueued); !errors.Is(err, ErrRetryNotFailed) {
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
		if err := a.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
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
		if err := a.Admit(context.Background(), pendingTrackOnly(t), scheduleQueued); !errors.Is(err, ErrReacquireNotReady) {
			t.Errorf("Admit = %v, want ErrReacquireNotReady", err)
		}
	})

	t.Run("failed track is not streamable", func(t *testing.T) {
		a := NewReacquireAdmission(newFakeCooldownStore())
		if err := a.Admit(context.Background(), failedTrack(t), scheduleQueued); !errors.Is(err, ErrReacquireNotReady) {
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
		if err := a.Admit(context.Background(), track, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
			t.Errorf("second Admit = %v, want ErrCooldownActive", err)
		}
	})
}

func TestReacquireAdmission(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	pending, _ := domain.NewTrack(userId, "T", "A", "B")
	if err := NewReacquireAdmission(newFakeCooldownStore()).Admit(context.Background(), pending, scheduleQueued); !errors.Is(err, ErrReacquireNotReady) {
		t.Errorf("pending track: err = %v, want ErrReacquireNotReady", err)
	}

	failed, err := domain.NewTrack(userId, "T", "A", "B")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	_ = failed.MarkFailed("boom")
	if err := NewReacquireAdmission(newFakeCooldownStore()).Admit(context.Background(), failed, scheduleQueued); !errors.Is(err, ErrReacquireNotReady) {
		t.Errorf("failed track: err = %v, want ErrReacquireNotReady", err)
	}

	ready, err := domain.NewTrack(userId, "T", "A", "B")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	_ = ready.MarkReady("u/a/b/c.mp3")
	admission := NewReacquireAdmission(newFakeCooldownStore())
	if err := admission.Admit(context.Background(), ready, scheduleQueued); err != nil {
		t.Fatalf("ready track: err = %v, want admitted", err)
	}
	if err := admission.Admit(context.Background(), ready, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
		t.Errorf("second call: err = %v, want ErrCooldownActive", err)
	}
}

func TestRetryAdmission_StillFailedOnlyWithCooldown(t *testing.T) {
	userId := shared.NewUserId(uuid.New())

	ready, err := domain.NewTrack(userId, "T", "A", "B")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	_ = ready.MarkReady("u/a/b/c.mp3")
	if err := NewRetryAdmission(newFakeCooldownStore()).Admit(context.Background(), ready, scheduleQueued); !errors.Is(err, ErrRetryNotFailed) {
		t.Errorf("ready track: err = %v, want ErrRetryNotFailed", err)
	}

	failed, err := domain.NewTrack(userId, "T", "A", "B")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	_ = failed.MarkFailed("boom")
	admission := NewRetryAdmission(newFakeCooldownStore())
	if err := admission.Admit(context.Background(), failed, scheduleQueued); err != nil {
		t.Fatalf("failed track: err = %v, want admitted", err)
	}
	if err := admission.Admit(context.Background(), failed, scheduleQueued); !errors.Is(err, ErrCooldownActive) {
		t.Errorf("second call: err = %v, want ErrCooldownActive", err)
	}
}

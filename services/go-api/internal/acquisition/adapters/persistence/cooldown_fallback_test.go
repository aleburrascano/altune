package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
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

// scriptedCooldownStore is a durable store whose calls fail with err while it
// is set, standing in for Postgres before and after migration 019 is applied.
type scriptedCooldownStore struct {
	mu       sync.Mutex
	err      error
	lastAt   map[string]time.Time
	reserves int
	releases int
}

func newScriptedCooldownStore(err error) *scriptedCooldownStore {
	return &scriptedCooldownStore{err: err, lastAt: make(map[string]time.Time)}
}

func (s *scriptedCooldownStore) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *scriptedCooldownStore) Reserve(_ context.Context, trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return time.Time{}, false, s.err
	}
	s.reserves++
	now, key := time.Now(), string(kind)+"/"+trackID.String()
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, nil
	}
	s.lastAt[key] = now
	return now, true, nil
}

func (s *scriptedCooldownStore) Release(_ context.Context, trackID domain.TrackId, kind ports.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.releases++
	key := string(kind) + "/" + trackID.String()
	if last, ok := s.lastAt[key]; ok && last.Equal(at) {
		delete(s.lastAt, key)
	}
	return nil
}

// missingTableErr is the error PgxCooldownStore returns before 019 is applied.
func missingTableErr() error {
	return fmt.Errorf("reserve cooldown: %w", &pgconn.PgError{
		Code:    "42P01",
		Message: `relation "acquisition_cooldowns" does not exist`,
	})
}

func failedTrack(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkFailed("boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	return track
}

func TestFallbackCooldownStore_MissingTableStillEnforcesCooldown(t *testing.T) {
	primary := newScriptedCooldownStore(missingTableErr())
	admission := service.NewRetryAdmission(NewFallbackCooldownStore(primary))
	track := failedTrack(t)
	ctx := context.Background()

	if err := admission.Admit(ctx, track, func() error { return nil }); err != nil {
		t.Fatalf("first Admit = %v, want nil (no 500 while the table is missing)", err)
	}
	if err := admission.Admit(ctx, track, func() error { return nil }); !errors.Is(err, service.ErrCooldownActive) {
		t.Errorf("second Admit = %v, want ErrCooldownActive", err)
	}
}

func TestFallbackCooldownStore_MissingTableRefundsRefusedSchedule(t *testing.T) {
	primary := newScriptedCooldownStore(missingTableErr())
	admission := service.NewRetryAdmission(NewFallbackCooldownStore(primary))
	track := failedTrack(t)
	ctx := context.Background()
	refused := errors.New("not queued")

	if err := admission.Admit(ctx, track, func() error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("refused Admit = %v, want the schedule error", err)
	}
	if err := admission.Admit(ctx, track, func() error { return nil }); err != nil {
		t.Errorf("Admit after refund = %v, want nil", err)
	}
	if err := admission.Admit(ctx, track, func() error { return nil }); !errors.Is(err, service.ErrCooldownActive) {
		t.Errorf("Admit after queued = %v, want ErrCooldownActive", err)
	}
}

func TestFallbackCooldownStore_MissingTableWindowElapses(t *testing.T) {
	primary := newScriptedCooldownStore(missingTableErr())
	store := NewFallbackCooldownStore(primary)
	now := time.Now()
	store.mem.now = func() time.Time { return now }
	track := failedTrack(t)
	ctx := context.Background()

	if _, ok, err := store.Reserve(ctx, track.ID, ports.CooldownRetry, time.Minute); err != nil || !ok {
		t.Fatalf("first Reserve = %v, %v; want ok", ok, err)
	}
	if _, ok, _ := store.Reserve(ctx, track.ID, ports.CooldownReacquire, time.Minute); !ok {
		t.Error("reacquire blocked by a retry admission; kinds must be independent")
	}
	now = now.Add(time.Minute)
	if _, ok, err := store.Reserve(ctx, track.ID, ports.CooldownRetry, time.Minute); err != nil || !ok {
		t.Errorf("Reserve after window = %v, %v; want ok", ok, err)
	}
}

// TestFallbackCooldownStore_ResumesDurablePathOnceTableExists covers the
// migration being applied while the process runs: the next call uses the
// durable store with no restart, and a reservation made in the degraded
// period still holds its window.
func TestFallbackCooldownStore_ResumesDurablePathOnceTableExists(t *testing.T) {
	primary := newScriptedCooldownStore(missingTableErr())
	admission := service.NewRetryAdmission(NewFallbackCooldownStore(primary))
	degradedTrack, freshTrack := failedTrack(t), failedTrack(t)
	ctx := context.Background()
	queued := func() error { return nil }

	if err := admission.Admit(ctx, degradedTrack, queued); err != nil {
		t.Fatalf("degraded Admit = %v, want nil", err)
	}

	primary.setErr(nil) // migration 019 applied

	if err := admission.Admit(ctx, freshTrack, queued); err != nil {
		t.Fatalf("Admit after migration = %v, want nil", err)
	}
	if primary.reserves != 1 {
		t.Errorf("durable reserves = %d, want 1 (the post-migration Admit must hit the database)", primary.reserves)
	}
	if err := admission.Admit(ctx, freshTrack, queued); !errors.Is(err, service.ErrCooldownActive) {
		t.Errorf("second durable Admit = %v, want ErrCooldownActive", err)
	}
	if err := admission.Admit(ctx, degradedTrack, queued); !errors.Is(err, service.ErrCooldownActive) {
		t.Errorf("Admit of a track reserved while degraded = %v, want ErrCooldownActive", err)
	}

	refused := errors.New("not queued")
	other := failedTrack(t)
	if err := admission.Admit(ctx, other, func() error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("refused durable Admit = %v, want schedule error", err)
	}
	if primary.releases != 1 {
		t.Errorf("durable releases = %d, want 1 (refund must reach the database)", primary.releases)
	}
}

func TestFallbackCooldownStore_OtherStoreErrorsPropagate(t *testing.T) {
	boom := errors.New("connection refused")
	for name, err := range map[string]error{
		"plain":      boom,
		"other code": &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			admission := service.NewRetryAdmission(NewFallbackCooldownStore(newScriptedCooldownStore(err)))
			called := false
			got := admission.Admit(context.Background(), failedTrack(t), func() error { called = true; return nil })
			if !errors.Is(got, err) {
				t.Errorf("Admit = %v, want wrapped %v", got, err)
			}
			if called {
				t.Error("schedule ran despite a store error")
			}
		})
	}
}

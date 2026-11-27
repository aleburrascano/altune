package memory_test

import (
	ports "altune/go-api/internal/playback/internal"
	"altune/go-api/internal/playback/internal/adapters/memory"
	"altune/go-api/internal/playback/internal/adapters/persistence"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

type noMetrics struct{}

func (noMetrics) CorruptStoredState()          {}
func (noMetrics) QueueStateOpTimedOut()        {}
func (noMetrics) QueueStateOpFailedTransient() {}
func (noMetrics) QueueStateStaleWrite()        {}

type subject struct {
	repo   ports.QueueStateRepository
	forget func(t *testing.T, user ids.UserId)
}

func queueStateSubjects() map[string]func(t *testing.T) subject {
	return map[string]func(t *testing.T) subject{
		"memory": func(*testing.T) subject {
			return subject{repo: memory.NewQueueStateRepo(), forget: func(*testing.T, ids.UserId) {}}
		},
		"postgres": func(t *testing.T) subject {
			dbtest.RequireIntegration(t)
			pool := dbtest.Pool(t)
			return subject{
				repo: persistence.NewPostgresQueueStateRepository(pool, noMetrics{}, time.Now),
				forget: func(t *testing.T, user ids.UserId) {
					t.Helper()
					if _, err := pool.Exec(context.Background(), `DELETE FROM playback_queue_state WHERE user_id = $1`, user.UUID()); err != nil {
						t.Errorf("remove the test row: %v", err)
					}
				},
			}
		},
	}
}

func newUser(t *testing.T, s subject) ids.UserId {
	t.Helper()
	user := ids.NewUserId(uuid.New())
	t.Cleanup(func() { s.forget(t, user) })
	return user
}

func stateOf(t *testing.T, user ids.UserId, at time.Time, tracks ...string) *domain.QueueState {
	t.Helper()
	state, err := domain.NewQueueState(domain.QueueStateInput{
		UserId:     user,
		TrackIds:   tracks,
		CurrentIdx: 1,
		PositionMs: 4200,
		RepeatMode: domain.RepeatAll,
	}, at)
	if err != nil {
		t.Fatalf("build a queue state: %v", err)
	}
	return state
}

func aTrack() string { return uuid.NewString() }

func TestQueueStateRepo_KeepsTheRepositoryContract(t *testing.T) {
	contract := map[string]func(t *testing.T, s subject){
		"a user with nothing stored is not found": func(t *testing.T, s subject) {
			_, err := s.repo.GetForUser(context.Background(), newUser(t, s))
			if !errors.Is(err, domain.ErrQueueStateNotFound) {
				t.Fatalf("GetForUser = %v, want ErrQueueStateNotFound", err)
			}
		},
		"a saved state reads back whole": func(t *testing.T, s subject) {
			user := newUser(t, s)
			want := stateOf(t, user, time.Now(), aTrack(), aTrack(), aTrack())

			if err := s.repo.Upsert(context.Background(), want); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			got, err := s.repo.GetForUser(context.Background(), user)
			if err != nil {
				t.Fatalf("GetForUser: %v", err)
			}
			if !slices.Equal(got.TrackIds, want.TrackIds) || got.CurrentIdx != want.CurrentIdx || got.PositionMs != want.PositionMs ||
				got.Shuffled != want.Shuffled || got.RepeatMode != want.RepeatMode || got.SourceId != want.SourceId {
				t.Fatalf("read back %+v, want %+v", got, want)
			}
		},
		"a newer save replaces the stored state": func(t *testing.T, s subject) {
			user := newUser(t, s)
			now := time.Now()
			first := stateOf(t, user, now.Add(-time.Second), aTrack(), aTrack())
			second := stateOf(t, user, now, aTrack(), aTrack(), aTrack())

			for _, state := range []*domain.QueueState{first, second} {
				if err := s.repo.Upsert(context.Background(), state); err != nil {
					t.Fatalf("Upsert: %v", err)
				}
			}
			got, _ := s.repo.GetForUser(context.Background(), user)

			if !slices.Equal(got.TrackIds, second.TrackIds) {
				t.Fatalf("stored tracks %v, want the newer save %v", got.TrackIds, second.TrackIds)
			}
		},
		"an older save is refused as stale and changes nothing": func(t *testing.T, s subject) {
			user := newUser(t, s)
			now := time.Now()
			stored := stateOf(t, user, now, aTrack(), aTrack())
			older := stateOf(t, user, now.Add(-time.Minute), aTrack(), aTrack(), aTrack())
			if err := s.repo.Upsert(context.Background(), stored); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			err := s.repo.Upsert(context.Background(), older)
			got, _ := s.repo.GetForUser(context.Background(), user)

			if !errors.Is(err, domain.ErrStaleQueueWrite) {
				t.Fatalf("Upsert(older) = %v, want ErrStaleQueueWrite", err)
			}
			if !slices.Equal(got.TrackIds, stored.TrackIds) {
				t.Fatalf("a refused save changed the stored tracks to %v", got.TrackIds)
			}
		},
		"a position moves within the stored queue and keeps its tracks": func(t *testing.T, s subject) {
			user := newUser(t, s)
			tracks := []string{aTrack(), aTrack(), aTrack()}
			if err := s.repo.Upsert(context.Background(), stateOf(t, user, time.Now().Add(-time.Second), tracks...)); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			position, err := domain.NewQueuePosition(domain.QueuePositionInput{UserId: user, CurrentIdx: 2, CurrentTrackId: tracks[2], PositionMs: 9000}, time.Now())
			if err != nil {
				t.Fatalf("build a position: %v", err)
			}

			if err := s.repo.UpdatePosition(context.Background(), position); err != nil {
				t.Fatalf("UpdatePosition: %v", err)
			}
			got, _ := s.repo.GetForUser(context.Background(), user)

			if got.CurrentIdx != 2 || got.PositionMs != 9000 || !slices.Equal(got.TrackIds, tracks) {
				t.Fatalf("after UpdatePosition got %+v, want idx 2, 9000ms and the same tracks", got)
			}
		},
		"a position for a track not at that index is a mismatch": func(t *testing.T, s subject) {
			user := newUser(t, s)
			tracks := []string{aTrack(), aTrack()}
			if err := s.repo.Upsert(context.Background(), stateOf(t, user, time.Now().Add(-time.Second), tracks...)); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			position, err := domain.NewQueuePosition(domain.QueuePositionInput{UserId: user, CurrentIdx: 0, CurrentTrackId: tracks[1], PositionMs: 1}, time.Now())
			if err != nil {
				t.Fatalf("build a position: %v", err)
			}

			if err := s.repo.UpdatePosition(context.Background(), position); !errors.Is(err, domain.ErrQueuePositionMismatch) {
				t.Fatalf("UpdatePosition = %v, want ErrQueuePositionMismatch", err)
			}
		},
		"a position for a user with nothing stored is a mismatch": func(t *testing.T, s subject) {
			user := newUser(t, s)
			position, err := domain.NewQueuePosition(domain.QueuePositionInput{UserId: user, CurrentIdx: 0, CurrentTrackId: aTrack(), PositionMs: 1}, time.Now())
			if err != nil {
				t.Fatalf("build a position: %v", err)
			}

			if err := s.repo.UpdatePosition(context.Background(), position); !errors.Is(err, domain.ErrQueuePositionMismatch) {
				t.Fatalf("UpdatePosition = %v, want ErrQueuePositionMismatch", err)
			}
		},
		"a deleted state is not found and deleting twice is fine": func(t *testing.T, s subject) {
			user := newUser(t, s)
			if err := s.repo.Upsert(context.Background(), stateOf(t, user, time.Now(), aTrack(), aTrack())); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			for range 2 {
				if err := s.repo.DeleteForUser(context.Background(), user); err != nil {
					t.Fatalf("DeleteForUser: %v", err)
				}
			}
			_, err := s.repo.GetForUser(context.Background(), user)

			if !errors.Is(err, domain.ErrQueueStateNotFound) {
				t.Fatalf("GetForUser after delete = %v, want ErrQueueStateNotFound", err)
			}
		},
		"a save handled before the forget does not bring the state back": func(t *testing.T, s subject) {
			user := newUser(t, s)
			if err := s.repo.DeleteForUser(context.Background(), user); err != nil {
				t.Fatalf("DeleteForUser: %v", err)
			}

			err := s.repo.Upsert(context.Background(), stateOf(t, user, time.Now().Add(-10*time.Second), aTrack(), aTrack()))
			_, got := s.repo.GetForUser(context.Background(), user)

			if !errors.Is(err, domain.ErrStaleQueueWrite) {
				t.Fatalf("Upsert stamped before the forget = %v, want ErrStaleQueueWrite", err)
			}
			if !errors.Is(got, domain.ErrQueueStateNotFound) {
				t.Fatalf("GetForUser after the refused save = %v, want ErrQueueStateNotFound", got)
			}
		},
		"users do not see each other's state": func(t *testing.T, s subject) {
			mine, theirs := newUser(t, s), newUser(t, s)
			if err := s.repo.Upsert(context.Background(), stateOf(t, mine, time.Now(), aTrack(), aTrack())); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			if _, err := s.repo.GetForUser(context.Background(), theirs); !errors.Is(err, domain.ErrQueueStateNotFound) {
				t.Fatalf("another user's GetForUser = %v, want ErrQueueStateNotFound", err)
			}
			if err := s.repo.DeleteForUser(context.Background(), theirs); err != nil {
				t.Fatalf("DeleteForUser: %v", err)
			}
			if _, err := s.repo.GetForUser(context.Background(), mine); err != nil {
				t.Fatalf("deleting another user's state removed mine: %v", err)
			}
		},
	}
	for impl, build := range queueStateSubjects() {
		for name, check := range contract {
			t.Run(impl+"/"+name, func(t *testing.T) { check(t, build(t)) })
		}
	}
}

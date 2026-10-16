package catalogbridge

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	catalogpersistence "altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newLedgerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	sharedtest.RequireIntegration(t)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertUnqueuedTrack(t *testing.T, pool *pgxpool.Pool) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString())
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track
}

func reclaimTrack(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) ports.Fence {
	t.Helper()
	var attempts int
	if err := pool.QueryRow(context.Background(),
		`UPDATE tracks SET acquisition_attempts = acquisition_attempts + 1, version = version + 1 WHERE id = $1 RETURNING acquisition_attempts`,
		trackID.UUID()).Scan(&attempts); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	return ports.Fence(attempts)
}

func rowState(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) (string, *string, *string) {
	t.Helper()
	var status string
	var audioRef, reason *string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_status, audio_ref, failure_reason FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&status, &audioRef, &reason); err != nil {
		t.Fatalf("select row: %v", err)
	}
	return status, audioRef, reason
}

func TestTrackLedger_OnPostgresAReclaimedTrackRefusesTheOldOwnerAndAcceptsTheNew(t *testing.T) {
	pool := newLedgerPool(t)
	ledger := NewTrackLedger(catalogpersistence.NewPgxTrackRepository(pool))
	ctx := context.Background()
	track := insertUnqueuedTrack(t, pool)

	old := reclaimTrack(t, pool, track.ID)
	current := reclaimTrack(t, pool, track.ID)

	if err := ledger.Complete(ctx, track.ID, track.UserId, old, ports.Completion{AudioRef: "old-owner-ref"}); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("old owner Complete = %v, want ErrLeaseLost", err)
	}
	if err := ledger.Fail(ctx, track.ID, track.UserId, old, "boom"); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("old owner Fail = %v, want ErrLeaseLost", err)
	}
	status, audioRef, reason := rowState(t, pool, track.ID)
	if status != "pending" || audioRef != nil || reason != nil {
		t.Fatalf("row = %s %v %v, want pending with no audio_ref and no failure_reason", status, audioRef, reason)
	}

	if err := ledger.Complete(ctx, track.ID, track.UserId, current, ports.Completion{AudioRef: "new-owner-ref"}); err != nil {
		t.Fatalf("current owner Complete = %v, want nil", err)
	}
	status, audioRef, _ = rowState(t, pool, track.ID)
	if status != "ready" || audioRef == nil || *audioRef != "new-owner-ref" {
		t.Errorf("row = %s %v, want ready on new-owner-ref", status, audioRef)
	}
}

type claimDuringUpdateStore struct {
	*catalogpersistence.PgxTrackRepository
	pool    *pgxpool.Pool
	t       *testing.T
	claimed bool
}

func (s *claimDuringUpdateStore) Update(ctx context.Context, track *domain.Track, expectedVersion int) error {
	if !s.claimed {
		s.claimed = true
		reclaimTrack(s.t, s.pool, track.ID)
	}
	return s.PgxTrackRepository.Update(ctx, track, expectedVersion)
}

func TestTrackLedger_OnPostgresAClaimBetweenReadAndUpdateFencesOutTheWriter(t *testing.T) {
	pool := newLedgerPool(t)
	track := insertUnqueuedTrack(t, pool)
	held := reclaimTrack(t, pool, track.ID)
	store := &claimDuringUpdateStore{PgxTrackRepository: catalogpersistence.NewPgxTrackRepository(pool), pool: pool, t: t}

	err := NewTrackLedger(store).Complete(context.Background(), track.ID, track.UserId, held, ports.Completion{AudioRef: "old-owner-ref"})

	if !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost once the version conflict re-reads the new owner's attempts", err)
	}
	status, audioRef, _ := rowState(t, pool, track.ID)
	if status != "pending" || audioRef != nil {
		t.Errorf("row = %s %v, want pending with no audio_ref", status, audioRef)
	}
}

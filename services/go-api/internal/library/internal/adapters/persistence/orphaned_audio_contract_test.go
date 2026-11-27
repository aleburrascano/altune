package persistence_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningOrphanQueue struct {
	*library.PostgresOrphanedAudioRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (q *cleaningOrphanQueue) RecordOrphanedAudio(ctx context.Context, orphan library.OrphanedAudio) error {
	cleanupOrphan(q.t, q.pool, orphan.AudioRef)
	return q.PostgresOrphanedAudioRepository.RecordOrphanedAudio(ctx, orphan)
}

func TestPostgresOrphanedAudioRepo_Contract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.OrphanedAudioQueueContract(t, func(t *testing.T) (library.OrphanedAudioQueue, func(*library.Track)) {
		seed := func(track *library.Track) {
			t.Helper()
			cleanupTrack(t, pool, track.ID, track.UserId)
			if _, _, err := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()).Add(context.Background(), track); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		return &cleaningOrphanQueue{PostgresOrphanedAudioRepository: library.NewPostgresOrphanedAudioRepository(pool, librarytest.DiscardDBCallMetrics()), t: t, pool: pool}, seed
	})
}

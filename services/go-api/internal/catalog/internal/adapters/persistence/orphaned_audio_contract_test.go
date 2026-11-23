package persistence_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningOrphanQueue struct {
	*catalog.PgxOrphanedAudioRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (q *cleaningOrphanQueue) RecordOrphanedAudio(ctx context.Context, orphan catalog.OrphanedAudio) error {
	cleanupOrphan(q.t, q.pool, orphan.AudioRef)
	return q.PgxOrphanedAudioRepository.RecordOrphanedAudio(ctx, orphan)
}

func TestPgxOrphanedAudioRepo_Contract(t *testing.T) {
	pool := sharedtest.Pool(t)
	contract.OrphanedAudioQueueContract(t, func(t *testing.T) (catalog.OrphanedAudioQueue, func(*catalog.Track)) {
		seed := func(track *catalog.Track) {
			t.Helper()
			cleanupTrack(t, pool, track.ID, track.UserId)
			if _, _, err := catalog.NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		return &cleaningOrphanQueue{PgxOrphanedAudioRepository: catalog.NewPgxOrphanedAudioRepository(pool), t: t, pool: pool}, seed
	})
}

package persistence

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningOrphanQueue struct {
	*PgxOrphanedAudioRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (q *cleaningOrphanQueue) RecordOrphanedAudio(ctx context.Context, orphan ports.OrphanedAudio) error {
	cleanupOrphan(q.t, q.pool, orphan.AudioRef)
	return q.PgxOrphanedAudioRepository.RecordOrphanedAudio(ctx, orphan)
}

func TestPgxOrphanedAudioRepo_Contract(t *testing.T) {
	pool := testPool(t)
	catalogtest.OrphanedAudioQueueContract(t, func(t *testing.T) (ports.OrphanedAudioQueue, func(*domain.Track)) {
		seed := func(track *domain.Track) {
			t.Helper()
			cleanupTrack(t, pool, track.ID, track.UserId)
			if _, _, err := NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		return &cleaningOrphanQueue{PgxOrphanedAudioRepository: NewPgxOrphanedAudioRepository(pool), t: t, pool: pool}, seed
	})
}

package persistence_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningTrackRepo struct {
	*catalog.PgxTrackRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningTrackRepo) Add(ctx context.Context, track *catalog.Track) (*catalog.Track, bool, error) {
	cleanupTrack(r.t, r.pool, track.ID, track.UserId)
	return r.PgxTrackRepository.Add(ctx, track)
}

func TestPgxTrackRepo_Contract(t *testing.T) {
	pool := testPool(t)
	contract.TrackRepositoryContract(t, func(t *testing.T) contract.TrackRepositoryUnderTest {
		return &cleaningTrackRepo{PgxTrackRepository: catalog.NewPgxTrackRepository(pool), t: t, pool: pool}
	})
}

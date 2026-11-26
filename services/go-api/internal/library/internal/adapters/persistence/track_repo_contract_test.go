package persistence_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contract"
	"altune/go-api/internal/shared/testdb"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningTrackRepo struct {
	*library.PostgresTrackRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningTrackRepo) Add(ctx context.Context, track *library.Track) (*library.Track, bool, error) {
	cleanupTrack(r.t, r.pool, track.ID, track.UserId)
	return r.PostgresTrackRepository.Add(ctx, track)
}

func TestPostgresTrackRepo_Contract(t *testing.T) {
	pool := testdb.Pool(t)
	contract.TrackRepositoryContract(t, func(t *testing.T) contract.TrackRepositoryUnderTest {
		return &cleaningTrackRepo{PostgresTrackRepository: library.NewPostgresTrackRepository(pool), t: t, pool: pool}
	})
}

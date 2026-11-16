package persistence

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningTrackRepo struct {
	*PgxTrackRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningTrackRepo) Add(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	cleanupTrack(r.t, r.pool, track.ID, track.UserId)
	return r.PgxTrackRepository.Add(ctx, track)
}

func TestPgxTrackRepo_Contract(t *testing.T) {
	pool := testPool(t)
	catalogtest.TrackRepositoryContract(t, func(t *testing.T) catalogtest.TrackRepositoryUnderTest {
		return &cleaningTrackRepo{PgxTrackRepository: NewPgxTrackRepository(pool), t: t, pool: pool}
	})
}

package persistence_test

import (
	"altune/go-api/internal/library/internal/adapters/persistence"
	"altune/go-api/internal/library/internal/contracttest"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningTrackRepo struct {
	*persistence.PostgresTrackRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningTrackRepo) Add(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	cleanupTrack(r.t, r.pool, track.ID(), track.UserId())
	return r.PostgresTrackRepository.Add(ctx, track)
}

func TestPostgresTrackRepo_LibraryCapContract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.TrackRepositoryLibraryCapContract(t, func(t *testing.T, libraryCap int) contracttest.TrackRepositoryUnderTest {
		repo := persistence.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics(), persistence.WithLibraryCap(libraryCap))
		return &cleaningTrackRepo{PostgresTrackRepository: repo, t: t, pool: pool}
	})
}

func TestPostgresTrackRepo_Contract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.TrackRepositoryContract(t, func(t *testing.T) contracttest.TrackRepositoryUnderTest {
		return &cleaningTrackRepo{PostgresTrackRepository: persistence.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()), t: t, pool: pool}
	})
}

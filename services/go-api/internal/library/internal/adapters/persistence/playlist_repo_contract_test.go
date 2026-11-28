package persistence_test

import (
	"altune/go-api/internal/library/internal/adapters/persistence"
	"altune/go-api/internal/library/internal/contracttest"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningPlaylistRepo struct {
	*persistence.PostgresPlaylistRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningPlaylistRepo) Create(ctx context.Context, playlist *domain.Playlist) error {
	cleanupPlaylist(r.t, r.pool, playlist.ID, playlist.UserId)
	return r.PostgresPlaylistRepository.Create(ctx, playlist)
}

func (r *cleaningPlaylistRepo) SeedTrack(t *testing.T, userId ids.UserId) domain.TrackId {
	return seedTrackForDB(context.Background(), t, r.pool, userId)
}

func TestPostgresPlaylistRepo_Contract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.PlaylistRepositoryContract(t, func(t *testing.T) contracttest.PlaylistRepositoryUnderTest {
		return &cleaningPlaylistRepo{PostgresPlaylistRepository: persistence.NewPostgresPlaylistRepository(pool, librarytest.DiscardDBCallMetrics()), t: t, pool: pool}
	})
}

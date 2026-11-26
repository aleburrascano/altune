package persistence_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningPlaylistRepo struct {
	*library.PostgresPlaylistRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningPlaylistRepo) Create(ctx context.Context, playlist *library.Playlist) error {
	cleanupPlaylist(r.t, r.pool, playlist.ID, playlist.UserId)
	return r.PostgresPlaylistRepository.Create(ctx, playlist)
}

func (r *cleaningPlaylistRepo) SeedTrack(t *testing.T, userId ids.UserId) library.TrackId {
	return seedTrackForDB(context.Background(), t, r.pool, userId)
}

func TestPostgresPlaylistRepo_Contract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.PlaylistRepositoryContract(t, func(t *testing.T) contracttest.PlaylistRepositoryUnderTest {
		return &cleaningPlaylistRepo{PostgresPlaylistRepository: library.NewPostgresPlaylistRepository(pool), t: t, pool: pool}
	})
}

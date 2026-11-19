package persistence_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/internal/adapters/contract"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningPlaylistRepo struct {
	*catalog.PgxPlaylistRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningPlaylistRepo) Create(ctx context.Context, playlist *catalog.Playlist) error {
	cleanupPlaylist(r.t, r.pool, playlist.ID, playlist.UserId)
	return r.PgxPlaylistRepository.Create(ctx, playlist)
}

func (r *cleaningPlaylistRepo) SeedTrack(t *testing.T, userId shared.UserId) catalog.TrackId {
	return seedTrackForDB(context.Background(), t, r.pool, userId)
}

func TestPgxPlaylistRepo_Contract(t *testing.T) {
	pool := testPool(t)
	contract.PlaylistRepositoryContract(t, func(t *testing.T) contract.PlaylistRepositoryUnderTest {
		return &cleaningPlaylistRepo{PgxPlaylistRepository: catalog.NewPgxPlaylistRepository(pool), t: t, pool: pool}
	})
}

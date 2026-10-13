package persistence

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cleaningPlaylistRepo struct {
	*PgxPlaylistRepository
	t    *testing.T
	pool *pgxpool.Pool
}

func (r *cleaningPlaylistRepo) Create(ctx context.Context, playlist *domain.Playlist) error {
	cleanupPlaylist(r.t, r.pool, playlist.ID, playlist.UserId)
	return r.PgxPlaylistRepository.Create(ctx, playlist)
}

func (r *cleaningPlaylistRepo) SeedTrack(t *testing.T, userId shared.UserId) domain.TrackId {
	return seedTrackForDB(context.Background(), t, r.pool, userId)
}

func TestPgxPlaylistRepo_Contract(t *testing.T) {
	pool := testPool(t)
	catalogtest.PlaylistRepositoryContract(t, func(t *testing.T) catalogtest.PlaylistRepositoryUnderTest {
		return &cleaningPlaylistRepo{PgxPlaylistRepository: NewPgxPlaylistRepository(pool), t: t, pool: pool}
	})
}

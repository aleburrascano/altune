package persistence_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestTrackForDB(t *testing.T, userId shared.UserId) *catalog.Track {
	t.Helper()
	track, err := catalog.NewTrack(userId, "Title-"+uuid.New().String()[:8], "Artist-"+uuid.New().String()[:8], "Album")
	if err != nil {
		t.Fatalf("newTestTrackForDB: %v", err)
	}
	return track
}

func cleanupTrack(t *testing.T, pool *pgxpool.Pool, id catalog.TrackId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM tracks WHERE id = $1 AND user_id = $2`,
			id.UUID(), userId.UUID())
	})
}

func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://catalog@127.0.0.1:1/catalog?connect_timeout=1")
	if err != nil {
		t.Fatalf("configure an unreachable pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestPlaylistForDB(t *testing.T, userId shared.UserId) *catalog.Playlist {
	t.Helper()
	pl, err := catalog.NewPlaylist(userId, "Playlist-"+uuid.New().String()[:8], time.Now())
	if err != nil {
		t.Fatalf("newTestPlaylistForDB: %v", err)
	}
	return pl
}

func seedTrackForDB(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) catalog.TrackId {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := catalog.NewPgxTrackRepository(pool).Add(ctx, track); err != nil {
		t.Fatalf("seedTrackForDB: %v", err)
	}
	return track.ID
}

func cleanupPlaylist(t *testing.T, pool *pgxpool.Pool, id catalog.PlaylistId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = $1`, id.UUID())
		_, _ = pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1 AND user_id = $2`, id.UUID(), userId.UUID())
	})
}

func seedPlaylistForDB(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) catalog.PlaylistId {
	t.Helper()
	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := catalog.NewPgxPlaylistRepository(pool).Create(ctx, pl); err != nil {
		t.Fatalf("seedPlaylistForDB: %v", err)
	}
	return pl.ID
}

func seedPlaylistWithTracks(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, n int) (*catalog.Playlist, []catalog.TrackId) {
	t.Helper()
	playlistRepo := catalog.NewPgxPlaylistRepository(pool)
	trackRepo := catalog.NewPgxTrackRepository(pool)

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}
	ids := make([]catalog.TrackId, n)
	for i := range ids {
		tr := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		if err := playlistRepo.AddTrack(ctx, userId, pl.ID, tr.ID); err != nil {
			t.Fatalf("AddTrack %d: %v", i, err)
		}
		ids[i] = tr.ID
	}
	return pl, ids
}

const bulkTracksSQL = `INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status, audio_ref)
	SELECT t.id, $2, 'Bulk ' || t.ord, 'Artist', 'bulk:' || t.id, 'ready', 'bulk/' || t.id || '.mp3'
	FROM unnest($1::uuid[]) WITH ORDINALITY AS t(id, ord)`

func seedBulkTracks(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, n int) []catalog.TrackId {
	t.Helper()
	ids := make([]catalog.TrackId, n)
	for i := range ids {
		ids[i] = catalog.NewTrackId()
	}
	insertBulkTracks(ctx, t, pool, userId, ids)
	return ids
}

func insertBulkTracks(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, ids []catalog.TrackId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})
	if _, err := pool.Exec(ctx, bulkTracksSQL, trackUUIDs(ids), userId.UUID()); err != nil {
		t.Fatalf("seed %d tracks: %v", len(ids), err)
	}
}

func trackUUIDs(ids []catalog.TrackId) []uuid.UUID {
	raw := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		raw[i] = id.UUID()
	}
	return raw
}

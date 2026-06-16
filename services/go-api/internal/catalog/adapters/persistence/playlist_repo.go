package persistence

import (
	"context"
	"errors"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.PlaylistRepository = (*PgxPlaylistRepository)(nil)

type PgxPlaylistRepository struct {
	pool *pgxpool.Pool
}

func NewPgxPlaylistRepository(pool *pgxpool.Pool) *PgxPlaylistRepository {
	return &PgxPlaylistRepository{pool: pool}
}

func (r *PgxPlaylistRepository) Create(ctx context.Context, playlist *domain.Playlist) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO playlists (id, user_id, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.CreatedAt, playlist.UpdatedAt,
	)
	return err
}

func (r *PgxPlaylistRepository) ListForUser(ctx context.Context, userId shared.UserId) ([]*domain.Playlist, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, name, created_at, updated_at FROM playlists WHERE user_id = $1 ORDER BY created_at DESC`,
		userId.UUID(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playlists []*domain.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, p)
	}
	return playlists, rows.Err()
}

func (r *PgxPlaylistRepository) GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, user_id, name, created_at, updated_at FROM playlists WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	var (
		pid       uuid.UUID
		uid       uuid.UUID
		name      string
		createdAt time.Time
		updatedAt time.Time
	)
	err := row.Scan(&pid, &uid, &name, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.Playlist{
		ID:        domain.PlaylistIdFromUUID(pid),
		UserId:    shared.NewUserId(uid),
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *PgxPlaylistRepository) GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error) {
	playlist, err := r.GetByID(ctx, id, userId)
	if err != nil || playlist == nil {
		return nil, nil, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT pt.track_id, pt.position,
			t.id, t.user_id, t.title, t.artist, t.album, t.duration_seconds,
			t.added_at, t.artwork_url, t.acquisition_status, t.dedup_key,
			t.year, t.genre, t.track_number, t.album_artist, t.isrc, t.audio_ref, t.failure_reason
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		WHERE pt.playlist_id = $1
		ORDER BY pt.position ASC`,
		id.UUID(),
	)
	if err != nil {
		return playlist, nil, err
	}
	defer rows.Close()

	var tracks []*domain.Track
	var playlistTracks []domain.PlaylistTrack
	for rows.Next() {
		var trackId uuid.UUID
		var position int
		var (
			tid           uuid.UUID
			tuid          uuid.UUID
			title, artist string
			album         *string
			durSecs       *float64
			addedAt       time.Time
			artworkURL    *string
			acqStatus     string
			dedupKey      string
			year          *int
			genre         *string
			trackNumber   *int
			albumArtist   *string
			isrc          *string
			audioRef      *string
			failureReason *string
		)
		err := rows.Scan(
			&trackId, &position,
			&tid, &tuid, &title, &artist, &album, &durSecs,
			&addedAt, &artworkURL, &acqStatus, &dedupKey,
			&year, &genre, &trackNumber, &albumArtist, &isrc, &audioRef, &failureReason,
		)
		if err != nil {
			return playlist, nil, err
		}

		status, _ := domain.ParseAcquisitionStatus(acqStatus)
		albumVal := ""
		if album != nil {
			albumVal = *album
		}

		tracks = append(tracks, &domain.Track{
			ID:                domain.TrackIdFromUUID(tid),
			UserId:            shared.NewUserId(tuid),
			Title:             title,
			Artist:            artist,
			Album:             albumVal,
			DurationSeconds:   durSecs,
			AddedAt:           addedAt,
			ArtworkURL:        artworkURL,
			AcquisitionStatus: status,
			DedupKey:          dedupKey,
			Year:              year,
			Genre:             genre,
			TrackNumber:       trackNumber,
			AlbumArtist:       albumArtist,
			ISRC:              isrc,
			AudioRef:          audioRef,
			FailureReason:     failureReason,
		})
		playlistTracks = append(playlistTracks, domain.PlaylistTrack{
			TrackId:  domain.TrackIdFromUUID(trackId),
			Position: position,
		})
	}
	playlist.Tracks = playlistTracks
	return playlist, tracks, rows.Err()
}

func (r *PgxPlaylistRepository) Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM playlists WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgxPlaylistRepository) Update(ctx context.Context, playlist *domain.Playlist) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE playlists SET name = $3, updated_at = $4 WHERE id = $1 AND user_id = $2`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.UpdatedAt,
	)
	return err
}

func (r *PgxPlaylistRepository) AddTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId, position int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES ($1, $2, $3)`,
		playlistId.UUID(), trackId.UUID(), position,
	)
	return err
}

func (r *PgxPlaylistRepository) RemoveTrack(ctx context.Context, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2`,
		playlistId.UUID(), trackId.UUID(),
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx,
		`UPDATE playlist_tracks SET position = sub.new_pos
		FROM (
			SELECT track_id, ROW_NUMBER() OVER (ORDER BY position) - 1 AS new_pos
			FROM playlist_tracks WHERE playlist_id = $1
		) sub
		WHERE playlist_tracks.playlist_id = $1 AND playlist_tracks.track_id = sub.track_id`,
		playlistId.UUID(),
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *PgxPlaylistRepository) ReorderTracks(ctx context.Context, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, t := range tracks {
		batch.Queue(
			`UPDATE playlist_tracks SET position = $3 WHERE playlist_id = $1 AND track_id = $2`,
			playlistId.UUID(), t.TrackId.UUID(), t.Position,
		)
	}
	br := tx.SendBatch(ctx, batch)
	for range tracks {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return err
		}
	}
	br.Close()

	return tx.Commit(ctx)
}

func (r *PgxPlaylistRepository) GetPreviewArtwork(ctx context.Context, playlistId domain.PlaylistId) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT t.artwork_url
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		WHERE pt.playlist_id = $1 AND t.artwork_url IS NOT NULL
		ORDER BY pt.position ASC
		LIMIT 4`,
		playlistId.UUID(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, rows.Err()
}

func scanPlaylist(rows pgx.Rows) (*domain.Playlist, error) {
	var (
		id        uuid.UUID
		userId    uuid.UUID
		name      string
		createdAt time.Time
		updatedAt time.Time
	)
	err := rows.Scan(&id, &userId, &name, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	return &domain.Playlist{
		ID:        domain.PlaylistIdFromUUID(id),
		UserId:    shared.NewUserId(userId),
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

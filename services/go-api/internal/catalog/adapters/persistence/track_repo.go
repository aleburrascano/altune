package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.TrackRepository = (*PgxTrackRepository)(nil)

type PgxTrackRepository struct {
	pool *pgxpool.Pool
}

func NewPgxTrackRepository(pool *pgxpool.Pool) *PgxTrackRepository {
	return &PgxTrackRepository{pool: pool}
}

func (r *PgxTrackRepository) Add(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var returnedID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO tracks (
			id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (user_id, dedup_key) DO NOTHING
		RETURNING id`,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.AddedAt, track.ArtworkURL, track.AcquisitionStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason,
	).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Dedup-key conflict: the row already exists — nothing was inserted (the
		// deferred rollback disposes the empty tx). Return the existing track so
		// the caller does not have to issue its own lookup.
		existing, lookupErr := r.GetByDedupKey(ctx, track.UserId, track.DedupKey)
		if lookupErr != nil {
			return nil, false, lookupErr
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	if err := writeTrackFeatured(ctx, tx, track.UserId.UUID(), track.ID.UUID(), track.FeaturedArtists); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return track, true, nil
}

func (r *PgxTrackRepository) GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
		FROM tracks WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	track, err := scanTrack(row)
	if err != nil || track == nil {
		return track, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, []*domain.Track{track}); err != nil {
		return nil, err
	}
	return track, nil
}

func (r *PgxTrackRepository) ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE user_id = $1`, userId.UUID()).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
		FROM tracks WHERE user_id = $1
		ORDER BY added_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		userId.UUID(), limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tracks []*domain.Track
	for rows.Next() {
		t, err := scanTrackFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, 0, err
	}
	return tracks, total, nil
}

// ListByIDs batch-loads the user's tracks matching ids in one query. Unknown or
// foreign ids are absent from the result; order is not guaranteed. Does not load
// featured credits (see the port doc) — the hot caller is audio-URL presigning,
// which only needs the acquisition status and audio ref.
func (r *PgxTrackRepository) ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	uuids := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = id.UUID()
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
		FROM tracks WHERE user_id = $1 AND id = ANY($2)`,
		userId.UUID(), uuids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []*domain.Track
	for rows.Next() {
		t, err := scanTrackFromRows(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (r *PgxTrackRepository) Update(ctx context.Context, track *domain.Track) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks SET
			title=$3, artist=$4, album=$5, duration_seconds=$6,
			artwork_url=$7, acquisition_status=$8, dedup_key=$9,
			year=$10, genre=$11, track_number=$12, album_artist=$13,
			isrc=$14, audio_ref=$15, failure_reason=$16
		WHERE id = $1 AND user_id = $2`,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.ArtworkURL, track.AcquisitionStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("track %s not found or was deleted", track.ID.String())
	}
	return nil
}

// SetTrackNumber fills the album position only when unset (WHERE track_number IS
// NULL), so it never clobbers a real value and is safe to call repeatedly.
func (r *PgxTrackRepository) SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks SET track_number=$3
		 WHERE id=$1 AND user_id=$2 AND track_number IS NULL`,
		id.UUID(), userId.UUID(), trackNumber,
	)
	if err != nil {
		return false, fmt.Errorf("set track number for %s: %w", id.String(), err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgxTrackRepository) Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM playlist_tracks WHERE track_id = $1`, id.UUID())
	if err != nil {
		return false, err
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM tracks WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PgxTrackRepository) GetByDedupKey(ctx context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason
		FROM tracks WHERE user_id = $1 AND dedup_key = $2`,
		userId.UUID(), dedupKey,
	)
	track, err := scanTrack(row)
	if err != nil || track == nil {
		return track, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, []*domain.Track{track}); err != nil {
		return nil, err
	}
	return track, nil
}

type scanner interface {
	Scan(dest ...any) error
}

// trackScanDest returns the 17 scan destinations for a track row (in the canonical
// column order used by every track SELECT) plus a builder that turns them into a
// domain.Track. Sharing it lets joins that carry extra columns (e.g. playlist
// position) reuse the exact same column list and construction.
func trackScanDest() (dest []any, build func() (*domain.Track, error)) {
	var (
		id            uuid.UUID
		userId        uuid.UUID
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

	dest = []any{
		&id, &userId, &title, &artist, &album, &durSecs,
		&addedAt, &artworkURL, &acqStatus, &dedupKey,
		&year, &genre, &trackNumber, &albumArtist, &isrc, &audioRef, &failureReason,
	}

	build = func() (*domain.Track, error) {
		status, err := domain.ParseAcquisitionStatus(acqStatus)
		if err != nil {
			return nil, err
		}

		albumVal := ""
		if album != nil {
			albumVal = *album
		}

		return &domain.Track{
			ID:                domain.TrackIdFromUUID(id),
			UserId:            shared.NewUserId(userId),
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
		}, nil
	}
	return dest, build
}

func scanTrackColumns(s scanner) (*domain.Track, error) {
	dest, build := trackScanDest()
	if err := s.Scan(dest...); err != nil {
		return nil, err
	}
	return build()
}

func scanTrack(row pgx.Row) (*domain.Track, error) {
	track, err := scanTrackColumns(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return track, err
}

func scanTrackFromRows(rows pgx.Rows) (*domain.Track, error) {
	return scanTrackColumns(rows)
}

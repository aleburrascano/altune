package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const trackColumns = `id, user_id, title, artist, album, duration_seconds,
	added_at, artwork_url, acquisition_status, dedup_key,
	year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url, rejected_source_keys, audio_version, acquisition_started_at`

var trackColumnsPrefixed = prefixColumns(trackColumns, "t.")

func prefixColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		name := strings.TrimLeft(part, " \n\t")
		lead := part[:len(part)-len(name)]
		parts[i] = lead + prefix + name
	}
	return strings.Join(parts, ",")
}

type PgxTrackRepository struct {
	pool pgxPool
}

func NewPgxTrackRepository(pool *pgxpool.Pool) *PgxTrackRepository {
	return &PgxTrackRepository{pool: pool}
}

func (r *PgxTrackRepository) Add(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var returnedID uuid.UUID
	// Bare ON CONFLICT DO NOTHING (no target) collapses on either unique
	// dimension: the content-derived (user_id, dedup_key) or the client-supplied
	// (user_id, idempotency_key) partial index. A no-rows result therefore means
	// one of those keys already exists, and the existing row is resolved below.
	err = tx.QueryRow(ctx,
		`INSERT INTO tracks (
			id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url,
			rejected_source_keys, audio_version, acquisition_started_at, idempotency_key
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.AddedAt, track.ArtworkURL, track.AcquisitionStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt, track.IdempotencyKey,
	).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// The insert was a no-op: a conflicting row already exists. Release this
		// transaction's connection before the follow-up lookup, which draws a
		// separate connection from the pool — holding both at once would exhaust
		// a small pool under concurrent conflicting inserts and deadlock.
		_ = tx.Rollback(ctx)
		existing, lookupErr := r.resolveConflictingTrack(ctx, track)
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
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	row := r.pool.QueryRow(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	return scanTrack(row)
}

func (r *PgxTrackRepository) ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE user_id = $1
		ORDER BY added_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		userId.UUID(), limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	tracks, err := collectTracks(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	total, err := pageTotal(ctx, r.pool, limit, offset, len(tracks),
		`SELECT count(*) FROM tracks WHERE user_id = $1`, userId.UUID())
	if err != nil {
		return nil, 0, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, 0, err
	}
	return tracks, total, nil
}

func (r *PgxTrackRepository) ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	uuids := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = id.UUID()
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE user_id = $1 AND id = ANY($2)`,
		userId.UUID(), uuids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectTracks(rows)
}

func (r *PgxTrackRepository) Update(ctx context.Context, track *domain.Track) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks SET
			title=$3, artist=$4, album=$5, duration_seconds=$6,
			artwork_url=$7, acquisition_status=$8, dedup_key=$9,
			year=$10, genre=$11, track_number=$12, album_artist=$13,
			isrc=$14, audio_ref=$15, failure_reason=$16, acquisition_provenance=$17, audio_source_url=$18,
			rejected_source_keys=$19, audio_version=$20, acquisition_started_at=$21
		WHERE id = $1 AND user_id = $2`,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.ArtworkURL, track.AcquisitionStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("track %s not found or was deleted", track.ID.String())
	}
	return nil
}

// SetTrackNumber enforces the write-once precondition of
// ports.TrackNumberSetter in SQL: the UPDATE matches only an owned row whose
// track_number IS NULL, so zero affected rows (updated=false) covers both an
// already-set number and a missing or foreign track.
func (r *PgxTrackRepository) SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

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

// FailStalePending transitions every pending track whose in-flight marker predates
// cutoff to failed, clearing the marker. These are tracks whose acquisition job was
// lost to a process that died mid-flight; moving them to failed lets the retry path
// reclaim them instead of leaving them stuck at pending forever.
func (r *PgxTrackRepository) FailStalePending(ctx context.Context, cutoff time.Time, reason string) (int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks
		 SET acquisition_status='failed', failure_reason=$2, acquisition_started_at=NULL
		 WHERE acquisition_status='pending'
		   AND acquisition_started_at IS NOT NULL
		   AND acquisition_started_at < $1`,
		cutoff, reason,
	)
	if err != nil {
		return 0, fmt.Errorf("fail stale pending acquisitions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Delete removes the track row. Playlist-membership cleanup is owned by the
// playlist_tracks -> tracks foreign key (ON DELETE CASCADE, migration 001): the
// track delete atomically evicts the track from every playlist that references
// it, so this repository does not touch playlist_tracks or its positions
// directly. That cascade is the only observable cross-aggregate side effect,
// and it runs inside this transaction, preserving the delete's atomicity.
func (r *PgxTrackRepository) Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback(ctx)

	deleted, ref, err := deleteTrackRow(ctx, tx, id, userId)
	if err != nil {
		return false, nil, err
	}
	if !deleted {
		return false, nil, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return false, nil, err
	}
	return deleted, ref, nil
}

func deleteTrackRow(ctx context.Context, tx pgx.Tx, id domain.TrackId, userId shared.UserId) (bool, *string, error) {
	var ref *string
	err := tx.QueryRow(ctx,
		`DELETE FROM tracks WHERE id = $1 AND user_id = $2 RETURNING audio_ref`,
		id.UUID(), userId.UUID(),
	).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	return true, ref, nil
}

// resolveConflictingTrack finds the row that made an insert a no-op. A supplied
// idempotency key is authoritative: a retry of one logical save must return the
// row that first landed under that key, even if the content differs. When no key
// was sent (or nothing matches it), the content-derived dedup key is the seam.
func (r *PgxTrackRepository) resolveConflictingTrack(ctx context.Context, track *domain.Track) (*domain.Track, error) {
	if track.IdempotencyKey != nil {
		existing, err := r.GetByIdempotencyKey(ctx, track.UserId, *track.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	return r.GetByDedupKey(ctx, track.UserId, track.DedupKey)
}

func (r *PgxTrackRepository) GetByIdempotencyKey(ctx context.Context, userId shared.UserId, idempotencyKey string) (*domain.Track, error) {
	return r.getTrackByUniqueKey(ctx, userId, "idempotency_key", idempotencyKey)
}

func (r *PgxTrackRepository) GetByDedupKey(ctx context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error) {
	return r.getTrackByUniqueKey(ctx, userId, "dedup_key", dedupKey)
}

// getTrackByUniqueKey loads the single track a user owns under one of the tracks
// table's unique keys. column is a fixed identifier chosen by the caller (never
// user input), so interpolating it into the query is safe; value is parameterized.
func (r *PgxTrackRepository) getTrackByUniqueKey(ctx context.Context, userId shared.UserId, column, value string) (*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	row := r.pool.QueryRow(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE user_id = $1 AND `+column+` = $2`,
		userId.UUID(), value,
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

// maxOwnedTrackRefs bounds the rows returned by ListOwnedTrackRefs at the
// catalog module's read cap (domain.MaxLibraryPageSize). It is a var so tests
// can exercise the bound without inserting the full cap.
var maxOwnedTrackRefs = domain.MaxLibraryPageSize

func (r *PgxTrackRepository) ListOwnedTrackRefs(
	ctx context.Context,
	userId shared.UserId,
) ([]domain.OwnedTrackRef, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, title, artist, acquisition_status, track_number
		FROM tracks WHERE user_id = $1
		ORDER BY id
		LIMIT $2`,
		userId.UUID(), maxOwnedTrackRefs,
	)
	if err != nil {
		return nil, fmt.Errorf("list owned track refs: %w", err)
	}
	defer rows.Close()

	refs := []domain.OwnedTrackRef{}
	for rows.Next() {
		var ref domain.OwnedTrackRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Artist, &ref.AcquisitionStatus, &ref.TrackNumber); err != nil {
			return nil, fmt.Errorf("scan owned track ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

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
		provenance    *string
		sourceURL     *string
		rejectedKeys  []string
		audioVersion  *string
		startedAt     *time.Time
	)

	dest = []any{
		&id, &userId, &title, &artist, &album, &durSecs,
		&addedAt, &artworkURL, &acqStatus, &dedupKey,
		&year, &genre, &trackNumber, &albumArtist, &isrc, &audioRef, &failureReason, &provenance, &sourceURL,
		&rejectedKeys, &audioVersion, &startedAt,
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

		versionVal := ""
		if audioVersion != nil {
			versionVal = *audioVersion
		}

		return &domain.Track{
			ID:                    domain.TrackIdFromUUID(id),
			UserId:                shared.NewUserId(userId),
			Title:                 title,
			Artist:                artist,
			Album:                 albumVal,
			DurationSeconds:       durSecs,
			AddedAt:               addedAt,
			ArtworkURL:            artworkURL,
			AcquisitionStatus:     status,
			DedupKey:              dedupKey,
			Year:                  year,
			Genre:                 genre,
			TrackNumber:           trackNumber,
			AlbumArtist:           albumArtist,
			ISRC:                  isrc,
			AudioRef:              audioRef,
			AudioVersion:          versionVal,
			FailureReason:         failureReason,
			AcquisitionProvenance: provenance,
			AudioSourceURL:        sourceURL,
			RejectedSourceKeys:    rejectedKeys,
			AcquisitionStartedAt:  startedAt,
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

func collectTracks(rows pgx.Rows) ([]*domain.Track, error) {
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

// pageTotal resolves the exact total row count for one LIMIT/OFFSET page of a
// library list, without a COUNT(*) OVER () window on the page query.
//
// The window forced Postgres to materialize and sort every one of the user's
// matching rows (full width, spilling to disk for large libraries) before the
// LIMIT applied, so a 50-row page cost O(library). Without it the page query
// can walk a sort-order index (migrations/020_track_library_indexes.sql) and
// stop after offset+limit rows. The total is then derived in two ways:
//
//   - A short page (fewer rows than the limit) is the last page, so the total
//     is exactly offset+got and no second query runs. An empty page past offset
//     0 is ambiguous (offset may overshoot), so it falls through to a count.
//   - Otherwise one narrow count(*) runs over the same filter. Unfiltered, it is
//     an index-only scan on a user_id-prefixed index; it is still O(matches),
//     but far cheaper than a full-width window sort, and it keeps the response's
//     exact Total and HasMore semantics unchanged for clients.
//
// Tradeoff: the page and the count are two statements, not one snapshot, so a
// concurrent add/delete can skew them by a row. A non-empty page clamps the count
// to at least offset+got so it is never reported as larger than its total (an
// empty page past the end keeps the true count). Approximate
// (reltuples) counts were rejected because they are table-wide, not per user;
// keyset pagination was rejected because it changes the API's response shape.
func pageTotal(ctx context.Context, pool pgxPool, limit, offset, got int, countSQL string, args ...any) (int, error) {
	if limit > 0 && got < limit && (got > 0 || offset == 0) {
		return offset + got, nil
	}
	var total int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count tracks: %w", err)
	}
	if got > 0 && total < offset+got {
		total = offset + got
	}
	return total, nil
}

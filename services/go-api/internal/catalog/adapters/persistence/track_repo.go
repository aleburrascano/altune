package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const trackColumns = `id, user_id, title, artist, album, duration_seconds,
	added_at, artwork_url, acquisition_status, dedup_key,
	year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url, rejected_source_keys, audio_version, acquisition_started_at, acquisition_confidence, acquisition_evidence, version`

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
	err = tx.QueryRow(ctx,
		`INSERT INTO tracks (
			id, user_id, title, artist, album, duration_seconds,
			added_at, artwork_url, acquisition_status, dedup_key,
			year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url,
			rejected_source_keys, audio_version, acquisition_started_at, idempotency_key,
			acquisition_confidence, acquisition_evidence, acquisition_available_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,
			CASE WHEN $9::text = 'pending' THEN now() END)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.AddedAt, track.ArtworkURL, track.AcquisitionStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt, track.IdempotencyKey,
		track.ConfidenceScore, track.EvidenceJSON,
	).Scan(&returnedID)

	if errors.Is(err, pgx.ErrNoRows) {
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
	track, err := scanTrack(row)
	return track, classifyDBError(err)
}

func (r *PgxTrackRepository) AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var inUse bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tracks WHERE audio_ref = $1 AND id <> $2)`,
		audioRef, excludeTrackID.UUID(),
	).Scan(&inUse)
	if err != nil {
		return false, classifyDBError(err)
	}
	return inUse, nil
}

func (r *PgxTrackRepository) CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var held int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT 1 FROM tracks WHERE user_id = $1 LIMIT $2) capped`,
		userId.UUID(), atMost,
	).Scan(&held)
	if err != nil {
		return 0, classifyDBError(err)
	}
	return held, nil
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

func (r *PgxTrackRepository) Update(ctx context.Context, track *domain.Track, expectedVersion int) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var newVersion int
	err := r.pool.QueryRow(ctx,
		`UPDATE tracks SET
			duration_seconds=$3, acquisition_status=$4, audio_ref=$5,
			failure_reason=$6, acquisition_provenance=$7, audio_source_url=$8,
			rejected_source_keys=$9, audio_version=$10, acquisition_started_at=$11,
			acquisition_confidence=$12, acquisition_evidence=$13,
			version = version + 1
		WHERE id = $1 AND user_id = $2 AND version = $14
		RETURNING version`,
		track.ID.UUID(), track.UserId.UUID(),
		track.DurationSeconds, track.AcquisitionStatus.String(), track.AudioRef,
		track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt,
		track.ConfidenceScore, track.EvidenceJSON,
		expectedVersion,
	).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.classifyFailedCAS(ctx, track, expectedVersion)
	}
	if err != nil {
		return classifyDBError(err)
	}
	track.Version = newVersion
	return nil
}

func (r *PgxTrackRepository) classifyFailedCAS(ctx context.Context, track *domain.Track, expectedVersion int) error {
	var currentVersion int
	err := r.pool.QueryRow(ctx,
		`SELECT version FROM tracks WHERE id = $1 AND user_id = $2`,
		track.ID.UUID(), track.UserId.UUID(),
	).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("track %s not found or was deleted", track.ID.String())
	}
	if err != nil {
		return classifyDBError(err)
	}
	return fmt.Errorf("%w: track %s expected version %d, found %d",
		ports.ErrTrackVersionConflict, track.ID.String(), expectedVersion, currentVersion)
}

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

func (r *PgxTrackRepository) FailStalePending(ctx context.Context, cutoff time.Time, reason string) (int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks
		 SET acquisition_status=$3, failure_reason=$2, acquisition_started_at=NULL
		 WHERE acquisition_status=$4
		   AND acquisition_started_at IS NOT NULL
		   AND acquisition_started_at < $1
		   AND acquisition_attempts > 0
		   AND (acquisition_lease_until IS NULL OR acquisition_lease_until < now())`,
		cutoff, reason,
		domain.AcquisitionFailed.String(), domain.AcquisitionPending.String(),
	)
	if err != nil {
		return 0, fmt.Errorf("fail stale pending acquisitions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

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

var maxOwnedTrackRefs = 50_000

var ownedTrackRefsPageSize = domain.MaxLibraryPageSize

func (r *PgxTrackRepository) ListOwnedTrackRefs(
	ctx context.Context,
	userId shared.UserId,
) ([]domain.OwnedTrackRef, error) {
	refs := []domain.OwnedTrackRef{}
	after := uuid.Nil
	for len(refs) < maxOwnedTrackRefs {
		limit := min(ownedTrackRefsPageSize, maxOwnedTrackRefs-len(refs))
		page, err := r.listOwnedTrackRefsPage(ctx, userId, after, limit)
		if err != nil {
			return nil, err
		}
		refs = append(refs, page...)
		if len(page) < limit {
			return refs, nil
		}
		if after, err = uuid.Parse(page[len(page)-1].ID); err != nil {
			return nil, fmt.Errorf("parse owned track ref id: %w", err)
		}
	}
	slog.Warn("owned track refs truncated at ceiling", "user_id", userId.UUID(), "ceiling", maxOwnedTrackRefs)
	return refs, nil
}

func (r *PgxTrackRepository) listOwnedTrackRefsPage(
	ctx context.Context,
	userId shared.UserId,
	after uuid.UUID,
	limit int,
) ([]domain.OwnedTrackRef, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, title, artist, acquisition_status, track_number
		FROM tracks WHERE user_id = $1 AND id > $2
		ORDER BY id
		LIMIT $3`,
		userId.UUID(), after, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list owned track refs: %w", err)
	}
	defer rows.Close()
	return scanOwnedTrackRefs(rows)
}

func scanOwnedTrackRefs(rows pgx.Rows) ([]domain.OwnedTrackRef, error) {
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
		confidence    *float64
		evidence      json.RawMessage
		version       int
	)

	dest = []any{
		&id, &userId, &title, &artist, &album, &durSecs,
		&addedAt, &artworkURL, &acqStatus, &dedupKey,
		&year, &genre, &trackNumber, &albumArtist, &isrc, &audioRef, &failureReason, &provenance, &sourceURL,
		&rejectedKeys, &audioVersion, &startedAt, &confidence, &evidence, &version,
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
			ConfidenceScore:       confidence,
			EvidenceJSON:          evidence,
			Version:               version,
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

package persistence

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/shared/database"
	sharedids "altune/go-api/internal/shared/ids"
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

const trackBaseColumns = `id, user_id, title, artist, album, duration_seconds,
	added_at, artwork_url, acquisition_status, dedup_key,
	year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url, rejected_source_keys, audio_version, acquisition_started_at, acquisition_confidence, acquisition_evidence, catalog_duration_seconds, catalog_mbid, catalog_explicit, catalog_sources, audio_length_mismatch, version`

var (
	trackColumns         = trackBaseColumns + ", " + fenceColumn("tracks")
	trackColumnsPrefixed = prefixColumns(trackBaseColumns, "t.") + ", " + fenceColumn("t")
)

func fenceColumn(alias string) string {
	return "COALESCE((SELECT j.attempts FROM acquisition_jobs j WHERE j.track_id = " + alias + ".id), 0)"
}

func prefixColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		name := strings.TrimLeft(part, " \n\t")
		lead := part[:len(part)-len(name)]
		parts[i] = lead + prefix + name
	}
	return strings.Join(parts, ",")
}

type PostgresTrackRepository struct {
	pool       *pgxpool.Pool
	libraryCap int
}

func NewPostgresTrackRepository(pool *pgxpool.Pool, opts ...func(*PostgresTrackRepository)) *PostgresTrackRepository {
	r := &PostgresTrackRepository{pool: pool, libraryCap: domain.MaxTracksPerUser}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithLibraryCap(libraryCap int) func(*PostgresTrackRepository) {
	return func(r *PostgresTrackRepository) {
		if libraryCap < 1 {
			panic("persistence: WithLibraryCap requires a positive cap")
		}
		r.libraryCap = libraryCap
	}
}

func (r *PostgresTrackRepository) Add(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	stored, created, err := r.addInTx(ctx, track)
	return stored, created, classifyDBError(err)
}

var errTrackAlreadyStored = errors.New("a track with the same identity is already stored")

func (r *PostgresTrackRepository) addInTx(ctx context.Context, track *domain.Track) (*domain.Track, bool, error) {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockLibraryOfUserSQL, track.UserId.UUID()); err != nil {
			return fmt.Errorf("lock the library of user %s: %w", track.UserId.UUID(), err)
		}
		if err := insertTrack(ctx, tx, track); err != nil {
			return err
		}
		if err := requireWithinLibraryCap(ctx, tx, track.UserId, r.libraryCap); err != nil {
			return err
		}
		return writeTrackFeatured(ctx, tx, track.UserId.UUID(), track.ID.UUID(), track.FeaturedArtists)
	})
	if errors.Is(err, errTrackAlreadyStored) {
		existing, err := r.resolveConflictingTrack(ctx, track)
		if errors.Is(err, domain.ErrTrackNotFound) {
			return nil, false, nil
		}
		return existing, false, err
	}
	if err != nil {
		return nil, false, err
	}
	return track, true, nil
}

const lockLibraryOfUserSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text, 2))`

func requireWithinLibraryCap(ctx context.Context, tx pgx.Tx, userId sharedids.UserId, libraryCap int) error {
	var held int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT 1 FROM tracks WHERE user_id = $1 LIMIT $2) capped`,
		userId.UUID(), libraryCap+1,
	).Scan(&held)
	if err != nil {
		return fmt.Errorf("count the library of user %s: %w", userId.UUID(), err)
	}
	if held > libraryCap {
		return domain.ErrLibraryFull
	}
	return nil
}

const insertTrackSQL = `INSERT INTO tracks (
		id, user_id, title, artist, album, duration_seconds,
		added_at, artwork_url, acquisition_status, dedup_key,
		year, genre, track_number, album_artist, isrc, audio_ref, failure_reason, acquisition_provenance, audio_source_url,
		rejected_source_keys, audio_version, acquisition_started_at, idempotency_key,
		acquisition_confidence, acquisition_evidence, catalog_duration_seconds, catalog_mbid, catalog_explicit, catalog_sources
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
	ON CONFLICT DO NOTHING
	RETURNING id`

func insertTrack(ctx context.Context, tx pgx.Tx, track *domain.Track) error {
	sources, err := catalogSourcesJSON(track.CatalogSources)
	if err != nil {
		return fmt.Errorf("insert track %s: %w", track.ID.UUID(), err)
	}
	var returnedID uuid.UUID
	err = tx.QueryRow(ctx, insertTrackSQL,
		track.ID.UUID(), track.UserId.UUID(),
		track.Title, track.Artist, track.Album, track.DurationSeconds,
		track.AddedAt, track.ArtworkURL, track.DownloadStatus.String(), track.DedupKey,
		track.Year, track.Genre, track.TrackNumber, track.AlbumArtist,
		track.ISRC, track.AudioRef, track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt, track.IdempotencyKey,
		track.ConfidenceScore, track.EvidenceJSON, track.CatalogDurationSeconds,
		track.CatalogMBID, track.CatalogExplicit, sources,
	).Scan(&returnedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errTrackAlreadyStored
	}
	if err != nil {
		return fmt.Errorf("insert track %s: %w", track.ID.UUID(), err)
	}
	return nil
}

func (r *PostgresTrackRepository) GetByID(ctx context.Context, id domain.TrackId, userId sharedids.UserId) (*domain.Track, error) {
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

func (r *PostgresTrackRepository) AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
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

func (r *PostgresTrackRepository) CountForUser(ctx context.Context, userId sharedids.UserId, atMost int) (int, error) {
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

func (r *PostgresTrackRepository) ListForUser(ctx context.Context, userId sharedids.UserId, limit, offset int) ([]*domain.Track, int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tracks, total, err := r.listForUser(ctx, userId, limit, offset)
	return tracks, total, classifyDBError(err)
}

var listTracksForUserSQL = `SELECT ` + trackColumns + `
	FROM tracks WHERE user_id = $1
	ORDER BY added_at DESC, id DESC
	LIMIT $2 OFFSET $3`

func (r *PostgresTrackRepository) listForUser(ctx context.Context, userId sharedids.UserId, limit, offset int) ([]*domain.Track, int, error) {
	rows, err := r.pool.Query(ctx, listTracksForUserSQL, userId.UUID(), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list tracks of user %s: %w", userId.UUID(), err)
	}
	tracks, err := collectTracks(rows)
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

func (r *PostgresTrackRepository) ListByIDs(ctx context.Context, userId sharedids.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tracks, err := r.listByIDs(ctx, userId, trackIdUUIDs(ids))
	return tracks, classifyDBError(err)
}

func (r *PostgresTrackRepository) listByIDs(ctx context.Context, userId sharedids.UserId, ids []uuid.UUID) ([]*domain.Track, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE user_id = $1 AND id = ANY($2)`,
		userId.UUID(), ids,
	)
	if err != nil {
		return nil, fmt.Errorf("load tracks of user %s by id: %w", userId.UUID(), err)
	}
	return collectTracks(rows)
}

const updateAcquisitionColumns = `duration_seconds=$3, acquisition_status=$4, audio_ref=$5,
			failure_reason=$6, acquisition_provenance=$7, audio_source_url=$8,
			rejected_source_keys=$9, audio_version=$10, acquisition_started_at=$11,
			acquisition_confidence=$12, acquisition_evidence=$13,
			isrc=$14, audio_length_mismatch=$15,
			version = version + 1`

const updateRelabelColumns = `,
			title=$17, album=$18, year=$19, dedup_key=$20,
			catalog_mbid=$21, catalog_duration_seconds=$22`

func updateStatement(track *domain.Track, expectedVersion int) (string, []any) {
	set := updateAcquisitionColumns
	args := []any{
		track.ID.UUID(), track.UserId.UUID(),
		track.DurationSeconds, track.DownloadStatus.String(), track.AudioRef,
		track.FailureReason, track.AcquisitionProvenance, track.AudioSourceURL,
		track.RejectedSourceKeys, track.AudioVersion, track.AcquisitionStartedAt,
		track.ConfidenceScore, track.EvidenceJSON,
		track.ISRC, track.AudioLengthMismatch,
		expectedVersion,
	}
	if track.Relabelled() {
		set += updateRelabelColumns
		args = append(args,
			track.Title, track.Album, track.Year, track.DedupKey,
			track.CatalogMBID, track.CatalogDurationSeconds)
	}
	return set, args
}

func (r *PostgresTrackRepository) Update(ctx context.Context, track *domain.Track, expectedVersion int) error {
	return r.update(ctx, track, expectedVersion, 0)
}

func (r *PostgresTrackRepository) UpdateFenced(ctx context.Context, track *domain.Track, expectedVersion, fence int) error {
	return r.update(ctx, track, expectedVersion, fence)
}

const fenceClause = ` AND ($%d::int = 0 OR acquisition_attempts = 0 OR acquisition_attempts = $%d)`

func (r *PostgresTrackRepository) update(ctx context.Context, track *domain.Track, expectedVersion, fence int) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	set, args := updateStatement(track, expectedVersion)
	where := `id = $1 AND user_id = $2 AND version = $16`
	if fence > 0 {
		args = append(args, fence)
		where += fmt.Sprintf(fenceClause, len(args), len(args))
	}
	var newVersion int
	err := r.pool.QueryRow(ctx,
		`UPDATE tracks SET `+set+`
		WHERE `+where+`
		RETURNING version`,
		args...,
	).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.classifyFailedCAS(ctx, track, expectedVersion, fence)
	}
	if err != nil {
		return classifyDBError(err)
	}
	track.Version = newVersion
	return nil
}

func (r *PostgresTrackRepository) classifyFailedCAS(ctx context.Context, track *domain.Track, expectedVersion, fence int) error {
	var currentVersion, currentFence int
	err := r.pool.QueryRow(ctx,
		`SELECT version, acquisition_attempts FROM tracks WHERE id = $1 AND user_id = $2`,
		track.ID.UUID(), track.UserId.UUID(),
	).Scan(&currentVersion, &currentFence)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("track %s not found or was deleted", track.ID.String())
	}
	if err != nil {
		return classifyDBError(err)
	}
	if fence > 0 && currentFence > 0 && currentFence != fence {
		return fmt.Errorf("%w: track %s held by attempt %d, caller holds %d",
			ports.ErrTrackFenceSuperseded, track.ID.String(), currentFence, fence)
	}
	return fmt.Errorf("%w: track %s expected version %d, found %d",
		ports.ErrTrackVersionConflict, track.ID.String(), expectedVersion, currentVersion)
}

func (r *PostgresTrackRepository) Requeue(ctx context.Context, id domain.TrackId) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	if _, err := r.pool.Exec(ctx,
		`UPDATE tracks SET
			acquisition_status = CASE WHEN acquisition_status = 'failed' THEN 'pending' ELSE acquisition_status END,
			acquisition_started_at = CASE WHEN acquisition_status = 'failed' THEN now() ELSE acquisition_started_at END,
			failure_reason = '',
			version = version + 1
		 WHERE id = $1 AND (acquisition_status = 'failed' OR COALESCE(failure_reason, '') <> '')`,
		id.UUID(),
	); err != nil {
		return classifyDBError(fmt.Errorf("requeue track %s: %w", id.String(), err))
	}
	return nil
}

func (r *PostgresTrackRepository) SetTrackNumber(ctx context.Context, id domain.TrackId, userId sharedids.UserId, trackNumber int) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE tracks SET track_number=$3
		 WHERE id=$1 AND user_id=$2 AND track_number IS NULL`,
		id.UUID(), userId.UUID(), trackNumber,
	)
	if err != nil {
		return false, classifyDBError(fmt.Errorf("set track number for %s: %w", id.String(), err))
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresTrackRepository) Delete(ctx context.Context, id domain.TrackId, userId sharedids.UserId) (deleted bool, audioRef *string, err error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	deleted, audioRef, err = r.deleteTrackRow(ctx, id, userId)
	return deleted, audioRef, classifyDBError(err)
}

func (r *PostgresTrackRepository) deleteTrackRow(ctx context.Context, id domain.TrackId, userId sharedids.UserId) (bool, *string, error) {
	var ref *string
	err := r.pool.QueryRow(ctx,
		`DELETE FROM tracks WHERE id = $1 AND user_id = $2 RETURNING audio_ref`,
		id.UUID(), userId.UUID(),
	).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("delete track %s: %w", id.UUID(), err)
	}
	return true, ref, nil
}

func (r *PostgresTrackRepository) resolveConflictingTrack(ctx context.Context, track *domain.Track) (*domain.Track, error) {
	if track.IdempotencyKey != nil {
		existing, err := r.GetByIdempotencyKey(ctx, track.UserId, *track.IdempotencyKey)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, domain.ErrTrackNotFound) {
			return nil, err
		}
	}
	return r.GetByDedupKey(ctx, track.UserId, track.DedupKey)
}

func (r *PostgresTrackRepository) GetByIdempotencyKey(ctx context.Context, userId sharedids.UserId, idempotencyKey string) (*domain.Track, error) {
	return r.getTrackByUniqueKey(ctx, userId, "idempotency_key", idempotencyKey)
}

func (r *PostgresTrackRepository) GetByDedupKey(ctx context.Context, userId sharedids.UserId, dedupKey string) (*domain.Track, error) {
	return r.getTrackByUniqueKey(ctx, userId, "dedup_key", dedupKey)
}

func (r *PostgresTrackRepository) getTrackByUniqueKey(ctx context.Context, userId sharedids.UserId, column, value string) (*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	track, err := r.findTrackByUniqueKey(ctx, userId, column, value)
	return track, classifyDBError(err)
}

func (r *PostgresTrackRepository) findTrackByUniqueKey(ctx context.Context, userId sharedids.UserId, column, value string) (*domain.Track, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+trackColumns+`
		FROM tracks WHERE user_id = $1 AND `+column+` = $2`,
		userId.UUID(), value,
	)
	track, err := scanTrack(row)
	if err != nil {
		return nil, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, []*domain.Track{track}); err != nil {
		return nil, err
	}
	return track, nil
}

const maxOwnedTrackRefs = 50_000

const ownedTrackRefsPageSize = domain.MaxLibraryPageSize

var _ = [1]struct{}{}[maxOwnedTrackRefs%ownedTrackRefsPageSize]

func (r *PostgresTrackRepository) ListOwnedTrackRefs(
	ctx context.Context,
	userId sharedids.UserId,
) ([]domain.OwnedTrackRef, error) {
	refs := []domain.OwnedTrackRef{}
	after := uuid.Nil
	for len(refs) < maxOwnedTrackRefs {
		page, err := r.listOwnedTrackRefsPage(ctx, userId, after)
		if err != nil {
			return nil, classifyDBError(err)
		}
		refs = append(refs, page...)
		if len(page) < ownedTrackRefsPageSize {
			return refs, nil
		}
		if after, err = uuid.Parse(page[len(page)-1].ID); err != nil {
			return nil, fmt.Errorf("parse owned track ref id: %w", err)
		}
	}
	slog.WarnContext(ctx, "owned track refs truncated at ceiling", "user_id", userId.UUID(), "ceiling", maxOwnedTrackRefs)
	return refs, nil
}

func (r *PostgresTrackRepository) listOwnedTrackRefsPage(
	ctx context.Context,
	userId sharedids.UserId,
	after uuid.UUID,
) ([]domain.OwnedTrackRef, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, title, artist, acquisition_status, track_number, COALESCE(isrc, ''), COALESCE(album, '')
		FROM tracks WHERE user_id = $1 AND id > $2
		ORDER BY id
		LIMIT $3`,
		userId.UUID(), after, ownedTrackRefsPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list owned track refs: %w", err)
	}
	return scanOwnedTrackRefs(rows)
}

const readyAudioTracksPageSQL = `SELECT id, user_id, title, artist, audio_ref
	FROM tracks
	WHERE acquisition_status = 'ready' AND audio_ref IS NOT NULL AND id > $1
	ORDER BY id
	LIMIT $2`

const maxReadyAudioTracks = 50_000

const readyAudioTracksPageSize = domain.MaxLibraryPageSize

var _ = [1]struct{}{}[maxReadyAudioTracks%readyAudioTracksPageSize]

func (r *PostgresTrackRepository) ListReadyAudioTracks(ctx context.Context) ([]ports.ReadyAudioTrack, error) {
	tracks := []ports.ReadyAudioTrack{}
	after := uuid.Nil
	for len(tracks) < maxReadyAudioTracks {
		page, err := r.listReadyAudioTracksPage(ctx, after)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, page...)
		if len(page) < readyAudioTracksPageSize {
			return tracks, nil
		}
		after = page[len(page)-1].Id.UUID()
	}
	slog.WarnContext(ctx, "ready audio tracks truncated at ceiling", "ceiling", maxReadyAudioTracks)
	return tracks, nil
}

func (r *PostgresTrackRepository) listReadyAudioTracksPage(
	ctx context.Context,
	after uuid.UUID,
) ([]ports.ReadyAudioTrack, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, readyAudioTracksPageSQL, after, readyAudioTracksPageSize)
	if err != nil {
		return nil, classifyDBError(fmt.Errorf("list ready audio tracks: %w", err))
	}
	return scanReadyAudioTracks(rows)
}

func scanReadyAudioTracks(rows pgx.Rows) ([]ports.ReadyAudioTrack, error) {
	page, err := pgx.CollectRows(rows, scanReadyAudioTrack)
	if err != nil {
		return nil, classifyDBError(fmt.Errorf("list ready audio tracks: %w", err))
	}
	return page, nil
}

func scanReadyAudioTrack(row pgx.CollectableRow) (ports.ReadyAudioTrack, error) {
	var id, userId uuid.UUID
	var t ports.ReadyAudioTrack
	if err := row.Scan(&id, &userId, &t.Title, &t.Artist, &t.AudioRef); err != nil {
		return ports.ReadyAudioTrack{}, fmt.Errorf("scan ready audio track: %w", err)
	}
	t.Id = domain.TrackIdFromUUID(id)
	t.UserId = sharedids.NewUserId(userId)
	return t, nil
}

func (r *PostgresTrackRepository) AudioRefsUnderPrefix(ctx context.Context, prefix string) (map[string]bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT audio_ref FROM tracks WHERE starts_with(audio_ref, $1)`,
		prefix,
	)
	if err != nil {
		return nil, classifyDBError(fmt.Errorf("list audio refs under prefix: %w", err))
	}
	return scanAudioRefSet(rows)
}

func scanAudioRefSet(rows pgx.Rows) (map[string]bool, error) {
	refs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, classifyDBError(fmt.Errorf("list audio refs under prefix: %w", err))
	}
	set := make(map[string]bool, len(refs))
	for _, ref := range refs {
		set[ref] = true
	}
	return set, nil
}

func scanOwnedTrackRefs(rows pgx.Rows) ([]domain.OwnedTrackRef, error) {
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.OwnedTrackRef, error) {
		var ref domain.OwnedTrackRef
		if err := row.Scan(&ref.ID, &ref.Title, &ref.Artist, &ref.DownloadStatus, &ref.TrackNumber, &ref.ISRC, &ref.Album); err != nil {
			return domain.OwnedTrackRef{}, fmt.Errorf("scan owned track ref: %w", err)
		}
		return ref, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect owned track refs: %w", err)
	}
	return refs, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func trackScanDest() (dest []any, build func() (*domain.Track, error)) {
	var (
		id              uuid.UUID
		userId          uuid.UUID
		title, artist   string
		album           *string
		durSecs         *float64
		addedAt         time.Time
		artworkURL      *string
		acqStatus       string
		dedupKey        string
		year            *int
		genre           *string
		trackNumber     *int
		albumArtist     *string
		isrc            *string
		audioRef        *string
		failureReason   *string
		provenance      *string
		sourceURL       *string
		rejectedKeys    []string
		audioVersion    *string
		startedAt       *time.Time
		confidence      *float64
		evidence        json.RawMessage
		catalogDurSecs  *float64
		catalogMBID     *string
		catalogExplicit *bool
		catalogSources  []byte
		lengthMismatch  bool
		version         int
		fence           int
	)

	dest = []any{
		&id, &userId, &title, &artist, &album, &durSecs,
		&addedAt, &artworkURL, &acqStatus, &dedupKey,
		&year, &genre, &trackNumber, &albumArtist, &isrc, &audioRef, &failureReason, &provenance, &sourceURL,
		&rejectedKeys, &audioVersion, &startedAt, &confidence, &evidence, &catalogDurSecs, &catalogMBID, &catalogExplicit, &catalogSources, &lengthMismatch, &version, &fence,
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

		sources, err := decodeCatalogSources(catalogSources)
		if err != nil {
			return nil, err
		}

		return &domain.Track{
			ID:                     domain.TrackIdFromUUID(id),
			UserId:                 sharedids.NewUserId(userId),
			Title:                  title,
			Artist:                 artist,
			Album:                  albumVal,
			DurationSeconds:        durSecs,
			AddedAt:                addedAt,
			ArtworkURL:             artworkURL,
			DownloadStatus:         status,
			DedupKey:               dedupKey,
			Year:                   year,
			Genre:                  genre,
			TrackNumber:            trackNumber,
			AlbumArtist:            albumArtist,
			ISRC:                   isrc,
			AudioRef:               audioRef,
			AudioVersion:           versionVal,
			FailureReason:          failureReason,
			AcquisitionProvenance:  provenance,
			AudioSourceURL:         sourceURL,
			RejectedSourceKeys:     rejectedKeys,
			AcquisitionStartedAt:   startedAt,
			ConfidenceScore:        confidence,
			EvidenceJSON:           evidence,
			CatalogDurationSeconds: catalogDurSecs,
			CatalogMBID:            catalogMBID,
			CatalogExplicit:        catalogExplicit,
			CatalogSources:         sources,
			AudioLengthMismatch:    lengthMismatch,
			Version:                version,
			AcquisitionFence:       fence,
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
		return nil, domain.ErrTrackNotFound
	}
	return track, err
}

func collectTracks(rows pgx.Rows) ([]*domain.Track, error) {
	tracks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Track, error) {
		return scanTrackColumns(row)
	})
	if err != nil {
		return nil, fmt.Errorf("collect tracks: %w", err)
	}
	return tracks, nil
}

func pageTotal(ctx context.Context, pool *pgxpool.Pool, limit, offset, got int, countSQL string, args ...any) (int, error) {
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

func catalogSourcesJSON(sources []domain.CatalogSource) ([]byte, error) {
	if len(sources) == 0 {
		return []byte("[]"), nil
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return nil, fmt.Errorf("encode catalog_sources: %w", err)
	}
	return encoded, nil
}

func decodeCatalogSources(raw []byte) ([]domain.CatalogSource, error) {
	var sources []domain.CatalogSource
	if err := json.Unmarshal(raw, &sources); err != nil {
		return nil, fmt.Errorf("decode catalog_sources: %w", err)
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return sources, nil
}

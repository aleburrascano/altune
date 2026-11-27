package persistence

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/database"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const playlistTrackCountSubquery = `(SELECT COUNT(*) FROM playlist_tracks pt WHERE pt.playlist_id = p.id)`

var (
	_ ports.PlaylistLifecycleRepository  = (*PostgresPlaylistRepository)(nil)
	_ ports.PlaylistMembershipRepository = (*PostgresPlaylistRepository)(nil)
)

type PostgresPlaylistRepository struct {
	pool    *pgxpool.Pool
	metrics ports.DBCallMetrics
}

func NewPostgresPlaylistRepository(pool *pgxpool.Pool, metrics ports.DBCallMetrics) *PostgresPlaylistRepository {
	requireDBCallMetrics("NewPostgresPlaylistRepository", metrics)
	return &PostgresPlaylistRepository{pool: pool, metrics: metrics}
}

func (r *PostgresPlaylistRepository) Create(ctx context.Context, playlist *domain.Playlist) error {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO playlists (id, user_id, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.CreatedAt, playlist.UpdatedAt,
	)
	return classifyDBError(err)
}

func (r *PostgresPlaylistRepository) CreateKeyed(ctx context.Context, playlist *domain.Playlist, idempotencyKey string) (*domain.Playlist, bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	for range keyedCreateAttempts {
		tag, err := r.pool.Exec(ctx,
			`INSERT INTO playlists (id, user_id, name, created_at, updated_at, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`,
			playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.CreatedAt, playlist.UpdatedAt, idempotencyKey,
		)
		if err != nil {
			return nil, false, classifyDBError(err)
		}
		if tag.RowsAffected() == 1 {
			return playlist, true, nil
		}
		existing, found, err := r.findByIdempotencyKey(ctx, playlist.UserId, idempotencyKey)
		if err != nil {
			return nil, false, err
		}
		if found {
			return existing, false, nil
		}
	}
	return nil, false, errKeyedCreateUnsettled
}

const keyedCreateAttempts = 2

var errKeyedCreateUnsettled = errors.New("create playlist by idempotency key: the winning row kept disappearing")

func (r *PostgresPlaylistRepository) GetByIdempotencyKey(ctx context.Context, userId sharedids.UserId, idempotencyKey string) (*domain.Playlist, bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()
	return r.findByIdempotencyKey(ctx, userId, idempotencyKey)
}

func (r *PostgresPlaylistRepository) findByIdempotencyKey(ctx context.Context, userId sharedids.UserId, idempotencyKey string) (*domain.Playlist, bool, error) {
	var (
		pid       uuid.UUID
		name      string
		createdAt time.Time
		updatedAt time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, created_at, updated_at FROM playlists WHERE user_id = $1 AND idempotency_key = $2`,
		userId.UUID(), idempotencyKey,
	).Scan(&pid, &name, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, classifyDBError(err)
	}
	return &domain.Playlist{ID: domain.PlaylistIdFromUUID(pid), UserId: userId, Name: name, CreatedAt: createdAt, UpdatedAt: updatedAt}, true, nil
}

func (r *PostgresPlaylistRepository) ListForUser(ctx context.Context, userId sharedids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT p.id, p.user_id, p.name, p.created_at, p.updated_at,
			`+playlistTrackCountSubquery+` AS track_count,
			COALESCE((
				SELECT array_agg(url) FROM (
					SELECT t.artwork_url AS url
					FROM playlist_tracks pt
					JOIN tracks t ON t.id = pt.track_id
					WHERE pt.playlist_id = p.id AND t.artwork_url IS NOT NULL
					GROUP BY t.artwork_url
					ORDER BY MIN(pt.position) ASC
					LIMIT $2
				) sub
			), '{}') AS preview_artwork
		FROM playlists p
		WHERE p.user_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $3 OFFSET $4`,
		userId.UUID(), domain.PreviewArtworkLimit, limit, offset,
	)
	if err != nil {
		return nil, classifyDBError(err)
	}
	stored, err := pgx.CollectRows(rows, pgx.RowToStructByName[playlistSummaryRow])
	if err != nil {
		return nil, classifyDBError(err)
	}
	summaries := make([]domain.PlaylistWithSummary, len(stored))
	for i, row := range stored {
		summaries[i] = row.withSummary()
	}
	return summaries, nil
}

type playlistSummaryRow struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	TrackCount     int
	PreviewArtwork []string
}

func (r playlistSummaryRow) withSummary() domain.PlaylistWithSummary {
	return domain.PlaylistWithSummary{
		Playlist: &domain.Playlist{
			ID:        domain.PlaylistIdFromUUID(r.ID),
			UserId:    sharedids.NewUserId(r.UserID),
			Name:      r.Name,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		},
		Summary: domain.PlaylistSummary{
			TrackCount:         r.TrackCount,
			PreviewArtworkURLs: r.PreviewArtwork,
		},
	}
}

func (r *PostgresPlaylistRepository) CountForUser(ctx context.Context, userId sharedids.UserId, atMost int) (int, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	var held int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT 1 FROM playlists WHERE user_id = $1 LIMIT $2) capped`,
		userId.UUID(), atMost,
	).Scan(&held)
	if err != nil {
		return 0, classifyDBError(err)
	}
	return held, nil
}

func (r *PostgresPlaylistRepository) GetByID(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	row := r.pool.QueryRow(ctx,
		`SELECT p.id, p.user_id, p.name, p.created_at, p.updated_at,
			`+playlistTrackCountSubquery+` AS track_count
		FROM playlists p WHERE p.id = $1 AND p.user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	var (
		pid        uuid.UUID
		uid        uuid.UUID
		name       string
		createdAt  time.Time
		updatedAt  time.Time
		trackCount int
	)
	err := row.Scan(&pid, &uid, &name, &createdAt, &updatedAt, &trackCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.PlaylistSummary{}, nil
	}
	if err != nil {
		return nil, domain.PlaylistSummary{}, classifyDBError(err)
	}
	playlist := &domain.Playlist{
		ID:        domain.PlaylistIdFromUUID(pid),
		UserId:    sharedids.NewUserId(uid),
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	return playlist, domain.PlaylistSummary{TrackCount: trackCount}, nil
}

const maxPlaylistTracks = domain.MaxPlaylistTracks

func (r *PostgresPlaylistRepository) GetWithTracks(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, []*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	playlist, _, err := r.GetByID(ctx, id, userId)
	if err != nil || playlist == nil {
		return nil, nil, classifyDBError(err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+trackColumnsPrefixed+`
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		WHERE pt.playlist_id = $1
		ORDER BY pt.position ASC, pt.track_id ASC
		LIMIT $2`,
		id.UUID(), maxPlaylistTracks,
	)
	if err != nil {
		return playlist, nil, classifyDBError(err)
	}
	tracks, err := collectTracks(rows)
	if err != nil {
		return playlist, nil, classifyDBError(err)
	}

	var playlistTracks []domain.PlaylistTrack
	for i, track := range tracks {
		playlistTracks = append(playlistTracks, domain.PlaylistTrack{
			TrackId:  track.ID(),
			Position: i,
		})
	}
	playlist.Tracks = playlistTracks
	return playlist, tracks, nil
}

func (r *PostgresPlaylistRepository) Delete(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`DELETE FROM playlists WHERE id = $1 AND user_id = $2`,
		id.UUID(), userId.UUID(),
	)
	if err != nil {
		return false, classifyDBError(err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresPlaylistRepository) Update(ctx context.Context, playlist *domain.Playlist) error {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE playlists SET name = $3, updated_at = $4 WHERE id = $1 AND user_id = $2`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.UpdatedAt,
	)
	if err != nil {
		return classifyDBError(err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrPlaylistNotOwned
	}
	return nil
}

func (r *PostgresPlaylistRepository) withOwnedPlaylistLock(ctx context.Context, playlistId domain.PlaylistId, userId sharedids.UserId, fn func(pgx.Tx) error) error {
	return classifyDBError(r.runOwnedPlaylistLock(ctx, playlistId, userId, fn))
}

func (r *PostgresPlaylistRepository) runOwnedPlaylistLock(ctx context.Context, playlistId domain.PlaylistId, userId sharedids.UserId, fn func(pgx.Tx) error) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockOwnedPlaylist(ctx, tx, playlistId, userId); err != nil {
			return err
		}
		return fn(tx)
	})
	if err != nil {
		return fmt.Errorf("run owned playlist lock: %w", err)
	}
	return nil
}

func lockOwnedPlaylist(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId, userId sharedids.UserId) error {
	var one int
	err := tx.QueryRow(ctx,
		`SELECT 1 FROM playlists WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		playlistId.UUID(), userId.UUID(),
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrPlaylistNotOwned
	}
	if err != nil {
		return fmt.Errorf("lock playlist %s: %w", playlistId.UUID(), err)
	}
	return nil
}

func (r *PostgresPlaylistRepository) Exists(ctx context.Context, playlistId domain.PlaylistId, userId sharedids.UserId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM playlists WHERE id = $1 AND user_id = $2)`,
		playlistId.UUID(), userId.UUID(),
	).Scan(&exists)
	return exists, classifyDBError(err)
}

func (r *PostgresPlaylistRepository) GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId sharedids.UserId) ([]domain.TrackId, bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT pt.track_id
		FROM playlists p
		LEFT JOIN playlist_tracks pt ON pt.playlist_id = p.id
		WHERE p.id = $1 AND p.user_id = $2
		ORDER BY pt.position ASC, pt.track_id ASC
		LIMIT $3`,
		playlistId.UUID(), userId.UUID(), maxPlaylistTracks,
	)
	if err != nil {
		return nil, false, classifyDBError(err)
	}
	members, err := pgx.CollectRows(rows, pgx.RowTo[pgtype.UUID])
	if err != nil {
		return nil, false, classifyDBError(err)
	}
	ids := []domain.TrackId{}
	for _, id := range members {
		if id.Valid {
			ids = append(ids, domain.TrackIdFromUUID(id.Bytes))
		}
	}
	return ids, len(members) > 0, nil
}

func (r *PostgresPlaylistRepository) AddTrack(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO playlist_tracks (playlist_id, track_id, position)
			VALUES ($1, $2, COALESCE((SELECT MAX(position) + 1 FROM playlist_tracks WHERE playlist_id = $1), 0))
			ON CONFLICT (playlist_id, track_id) DO NOTHING`,
			playlistId.UUID(), trackId.UUID(),
		)
		if err != nil {
			return fmt.Errorf("add track %s to playlist %s: %w", trackId.UUID(), playlistId.UUID(), err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrTrackAlreadyInPlaylist
		}
		return requireWithinTrackCap(ctx, tx, playlistId)
	})
	return missingTrackError(err)
}

func requireWithinTrackCap(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId) error {
	var total int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = $1`,
		playlistId.UUID(),
	).Scan(&total)
	if err != nil {
		return fmt.Errorf("count tracks of playlist %s: %w", playlistId.UUID(), err)
	}
	if err := domain.RequirePlaylistWithinCap(total); err != nil {
		return fmt.Errorf("playlist %s over its cap: %w", playlistId.UUID(), err)
	}
	return nil
}

const foreignKeyViolation = "23503"

func missingTrackError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return fmt.Errorf("%w: %w", ports.ErrTrackMissing, err)
	}
	return err
}

const addTracksSQL = `WITH requested AS (
	SELECT t.track_id, MIN(t.ord) AS ord
	FROM unnest($2::uuid[]) WITH ORDINALITY AS t(track_id, ord)
	GROUP BY t.track_id
), fresh AS (
	SELECT r.track_id, ROW_NUMBER() OVER (ORDER BY r.ord) AS rn
	FROM requested r
	WHERE NOT EXISTS (
		SELECT 1 FROM playlist_tracks pt
		WHERE pt.playlist_id = $1 AND pt.track_id = r.track_id
	)
)
INSERT INTO playlist_tracks (playlist_id, track_id, position)
SELECT $1, f.track_id, base.max_pos + f.rn
FROM fresh f
CROSS JOIN (
	SELECT COALESCE(MAX(position), -1) AS max_pos
	FROM playlist_tracks WHERE playlist_id = $1
) base
RETURNING track_id`

func (r *PostgresPlaylistRepository) AddTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	if len(trackIds) == 0 {
		return nil, nil
	}

	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	var added []domain.TrackId
	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, addTracksSQL, playlistId.UUID(), trackIdUUIDs(trackIds))
		if err != nil {
			return fmt.Errorf("insert tracks into playlist %s: %w", playlistId.UUID(), err)
		}
		inserted, err := collectUUIDSet(rows)
		if err != nil {
			return err
		}
		added = inRequestOrder(trackIds, inserted)
		if len(inserted) == 0 {
			return nil
		}
		return requireWithinTrackCap(ctx, tx, playlistId)
	})
	if err != nil {
		return nil, missingTrackError(err)
	}
	return added, nil
}

func (r *PostgresPlaylistRepository) RemoveTrack(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	removed := false
	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		var position int
		err := tx.QueryRow(ctx,
			`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2 RETURNING position`,
			playlistId.UUID(), trackId.UUID(),
		).Scan(&position)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove track %s from playlist %s: %w", trackId.UUID(), playlistId.UUID(), err)
		}
		removed = true
		_, err = tx.Exec(ctx,
			`UPDATE playlist_tracks SET position = position - 1 WHERE playlist_id = $1 AND position > $2`,
			playlistId.UUID(), position,
		)
		if err != nil {
			return fmt.Errorf("close the position gap in playlist %s: %w", playlistId.UUID(), err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (r *PostgresPlaylistRepository) RemoveTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	var removed []domain.TrackId
	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		gone, positions, err := deleteMemberships(ctx, tx, playlistId, trackIds)
		if err != nil || len(positions) == 0 {
			return err
		}
		removed = inRequestOrder(trackIds, gone)
		_, err = tx.Exec(ctx,
			`UPDATE playlist_tracks SET position = position - width_bucket(position, $2::int[])
			WHERE playlist_id = $1 AND position > $3`,
			playlistId.UUID(), positions, positions[0],
		)
		if err != nil {
			return fmt.Errorf("close the position gaps in playlist %s: %w", playlistId.UUID(), err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

func deleteMemberships(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId, trackIds []domain.TrackId) (map[uuid.UUID]bool, []int, error) {
	rows, err := tx.Query(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = ANY($2) RETURNING track_id, position`,
		playlistId.UUID(), trackIdUUIDs(trackIds),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("delete tracks from playlist %s: %w", playlistId.UUID(), err)
	}
	removed, err := pgx.CollectRows(rows, pgx.RowToStructByName[removedMembership])
	if err != nil {
		return nil, nil, fmt.Errorf("collect the removed memberships of playlist %s: %w", playlistId.UUID(), err)
	}
	gone := make(map[uuid.UUID]bool, len(removed))
	positions := make([]int, len(removed))
	for i, m := range removed {
		gone[m.TrackID] = true
		positions[i] = m.Position
	}
	sort.Ints(positions)
	return gone, positions, nil
}

type removedMembership struct {
	TrackID  uuid.UUID
	Position int
}

func (r *PostgresPlaylistRepository) ReorderTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(tracks))
	positions := make([]int, len(tracks))
	for i, t := range tracks {
		ids[i] = t.TrackId.UUID()
		positions[i] = t.Position
	}

	ctx, cancel := withDBTimeout(ctx, r.metrics)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		if err := requirePlanCoversMembership(ctx, tx, playlistId, ids); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`UPDATE playlist_tracks pt SET position = u.position
			FROM unnest($2::uuid[], $3::int[]) AS u(track_id, position)
			WHERE pt.playlist_id = $1 AND pt.track_id = u.track_id AND pt.position <> u.position`,
			playlistId.UUID(), ids, positions,
		)
		if err != nil {
			return fmt.Errorf("reorder playlist %s: %w", playlistId.UUID(), err)
		}
		return nil
	})
}

func requirePlanCoversMembership(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId, planned []uuid.UUID) error {
	rows, err := tx.Query(ctx,
		`SELECT track_id FROM playlist_tracks
		WHERE playlist_id = $1
		ORDER BY position ASC, track_id ASC
		LIMIT $2
		FOR UPDATE`,
		playlistId.UUID(), maxPlaylistTracks,
	)
	if err != nil {
		return fmt.Errorf("read the members of playlist %s: %w", playlistId.UUID(), err)
	}
	members, err := collectUUIDSet(rows)
	if err != nil {
		return err
	}
	if len(planned) != len(members) {
		return ports.ErrPlaylistChangedDuringReorder
	}
	for _, id := range planned {
		if !members[id] {
			return ports.ErrPlaylistChangedDuringReorder
		}
	}
	return nil
}

func trackIdUUIDs(ids []domain.TrackId) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = id.UUID()
	}
	return out
}

func collectUUIDSet(rows pgx.Rows) (map[uuid.UUID]bool, error) {
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("collect playlist track ids: %w", err)
	}
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

func inRequestOrder(requested []domain.TrackId, set map[uuid.UUID]bool) []domain.TrackId {
	out := make([]domain.TrackId, 0, len(set))
	seen := make(map[uuid.UUID]bool, len(set))
	for _, id := range requested {
		u := id.UUID()
		if set[u] && !seen[u] {
			seen[u] = true
			out = append(out, id)
		}
	}
	return out
}

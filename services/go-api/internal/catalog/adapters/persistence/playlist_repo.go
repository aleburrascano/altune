package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"maps"
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
	_ ports.PlaylistLifecycleRepository  = (*PgxPlaylistRepository)(nil)
	_ ports.PlaylistMembershipRepository = (*PgxPlaylistRepository)(nil)
)

type PgxPlaylistRepository struct {
	pool pgxPool
}

func NewPgxPlaylistRepository(pool *pgxpool.Pool) *PgxPlaylistRepository {
	return &PgxPlaylistRepository{pool: pool}
}

func (r *PgxPlaylistRepository) Create(ctx context.Context, playlist *domain.Playlist) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO playlists (id, user_id, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.CreatedAt, playlist.UpdatedAt,
	)
	return err
}

// ListForUser orders by created_at DESC with the id as a tiebreak: playlists
// created in the same microsecond would otherwise order arbitrarily, and two
// pages of an arbitrary order can repeat one row and never serve another.
func (r *PgxPlaylistRepository) ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error) {
	ctx, cancel := withDBTimeout(ctx)
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
		return nil, err
	}
	defer rows.Close()

	var result []domain.PlaylistWithSummary
	for rows.Next() {
		var (
			id         uuid.UUID
			uid        uuid.UUID
			name       string
			createdAt  time.Time
			updatedAt  time.Time
			trackCount int
			artwork    []string
		)
		err := rows.Scan(&id, &uid, &name, &createdAt, &updatedAt, &trackCount, &artwork)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.PlaylistWithSummary{
			Playlist: &domain.Playlist{
				ID:        domain.PlaylistIdFromUUID(id),
				UserId:    shared.NewUserId(uid),
				Name:      name,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			Summary: domain.PlaylistSummary{
				TrackCount:         trackCount,
				PreviewArtworkURLs: artwork,
			},
		})
	}
	return result, rows.Err()
}

// CountForUser counts the user's playlists, stopping at atMost: the count only
// gates the per-user cap, so the inner LIMIT bounds the rows it reads rather
// than scanning an account that is already far past it.
func (r *PgxPlaylistRepository) CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var held int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT 1 FROM playlists WHERE user_id = $1 LIMIT $2) capped`,
		userId.UUID(), atMost,
	).Scan(&held)
	if err != nil {
		return 0, err
	}
	return held, nil
}

func (r *PgxPlaylistRepository) GetByID(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, domain.PlaylistSummary, error) {
	ctx, cancel := withDBTimeout(ctx)
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
		return nil, domain.PlaylistSummary{}, err
	}
	playlist := &domain.Playlist{
		ID:        domain.PlaylistIdFromUUID(pid),
		UserId:    shared.NewUserId(uid),
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	return playlist, domain.PlaylistSummary{TrackCount: trackCount}, nil
}

// maxPlaylistTracks is one number on both sides of the playlist: the rows
// GetWithTracks and GetTrackOrder return, and the total size every membership
// insert is checked against under the playlist lock. They must agree — a
// playlist allowed past the read bound has a tail nothing can read or reorder.
// It is a var so tests can exercise both without inserting the full cap.
var maxPlaylistTracks = domain.MaxPlaylistTracks

func (r *PgxPlaylistRepository) GetWithTracks(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (*domain.Playlist, []*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	playlist, _, err := r.GetByID(ctx, id, userId)
	if err != nil || playlist == nil {
		return nil, nil, err
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
		return playlist, nil, err
	}
	defer rows.Close()

	tracks, err := collectTracks(rows)
	if err != nil {
		return playlist, nil, err
	}

	var playlistTracks []domain.PlaylistTrack
	for i, track := range tracks {
		playlistTracks = append(playlistTracks, domain.PlaylistTrack{
			TrackId:  track.ID,
			Position: i,
		})
	}
	playlist.Tracks = playlistTracks
	return playlist, tracks, nil
}

func (r *PgxPlaylistRepository) Delete(ctx context.Context, id domain.PlaylistId, userId shared.UserId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

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
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`UPDATE playlists SET name = $3, updated_at = $4 WHERE id = $1 AND user_id = $2`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrPlaylistNotOwned
	}
	return nil
}

// withOwnedPlaylistLock runs fn inside a transaction that first takes a row
// lock on the playlist, scoped to its owner. The lock serializes concurrent
// membership writes against the same playlist so position assignment reads a
// committed snapshot, never a stale one. The owner predicate makes the data
// layer itself refuse a write to a playlist userId does not own: if no row
// matches (missing playlist, or another tenant's), fn never runs and
// ports.ErrPlaylistNotOwned is returned. Because the matched row stays locked
// until commit, ownership cannot change (nor the playlist be deleted) between
// the check and the write.
func (r *PgxPlaylistRepository) withOwnedPlaylistLock(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var one int
	err = tx.QueryRow(ctx,
		`SELECT 1 FROM playlists WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		playlistId.UUID(), userId.UUID(),
	).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrPlaylistNotOwned
	}
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Exists reports whether the playlist exists and is owned by userId, touching
// only the playlist row.
func (r *PgxPlaylistRepository) Exists(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM playlists WHERE id = $1 AND user_id = $2)`,
		playlistId.UUID(), userId.UUID(),
	).Scan(&exists)
	return exists, err
}

// GetTrackOrder returns the playlist's track ids in position order, reading
// only playlist_tracks (no track columns) and bounded like GetWithTracks. The
// LEFT JOIN yields one NULL row for an owned empty playlist and no row at all
// for a missing or foreign one, so a single statement answers both questions.
func (r *PgxPlaylistRepository) GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId shared.UserId) ([]domain.TrackId, bool, error) {
	ctx, cancel := withDBTimeout(ctx)
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
		return nil, false, err
	}
	defer rows.Close()

	found := false
	ids := []domain.TrackId{}
	for rows.Next() {
		found = true
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		if id.Valid {
			ids = append(ids, domain.TrackIdFromUUID(id.Bytes))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return ids, found, nil
}

// AddTrack appends the track at the authoritative max(position)+1 computed under
// a playlist lock, so two simultaneous appends can never land on the same
// position. Membership is decided by the insert itself (ON CONFLICT on the
// primary key) rather than by loading the playlist: a track that is already a
// member yields domain.ErrTrackAlreadyInPlaylist with nothing written. An
// append past maxPlaylistTracks yields domain.ErrPlaylistFull, also with
// nothing written.
func (r *PgxPlaylistRepository) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO playlist_tracks (playlist_id, track_id, position)
			VALUES ($1, $2, COALESCE((SELECT MAX(position) + 1 FROM playlist_tracks WHERE playlist_id = $1), 0))
			ON CONFLICT (playlist_id, track_id) DO NOTHING`,
			playlistId.UUID(), trackId.UUID(),
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrTrackAlreadyInPlaylist
		}
		return requireWithinTrackCap(ctx, tx, playlistId)
	})
	return missingTrackError(err)
}

// requireWithinTrackCap re-counts the playlist inside the caller's locked
// transaction and refuses an insert that took it past maxPlaylistTracks. The
// refusal aborts the transaction, so the cap binds the rows that commit rather
// than the rows some earlier, unlocked read saw: two adds racing the last free
// slot cannot both take it. It runs after the insert because the insert is
// what decides how many of the requested ids were not already members.
func requireWithinTrackCap(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId) error {
	var total int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = $1`,
		playlistId.UUID(),
	).Scan(&total)
	if err != nil {
		return err
	}
	if total > maxPlaylistTracks {
		return domain.ErrPlaylistFull
	}
	return nil
}

// foreignKeyViolation is the SQLSTATE playlist_tracks raises when its track_id
// names a row that is not in tracks.
const foreignKeyViolation = "23503"

// missingTrackError marks a membership insert the track foreign key refused as
// ports.ErrTrackMissing: the track was deleted between the caller's lookup and
// the insert, which is a missing track rather than an internal fault. The
// original error stays in the chain.
func missingTrackError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return fmt.Errorf("%w: %w", ports.ErrTrackMissing, err)
	}
	return err
}

// addTracksSQL appends the requested ids that are not yet members, in first-
// occurrence request order, at a contiguous run of slots after max(position).
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

// AddTracks appends the given tracks in order, each at the authoritative
// max(position)+1 computed under a playlist lock. A single set-based insert
// skips ids already in the playlist (or repeated in the request) and assigns
// the rest a contiguous run of slots from the locked snapshot, so a concurrent
// add cannot wedge a duplicate position between them. It returns the inserted
// ids in request order. A batch whose new members would take the playlist past
// maxPlaylistTracks is refused whole, with domain.ErrPlaylistFull: a partial
// batch would leave the caller unable to say which half landed.
func (r *PgxPlaylistRepository) AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	if len(trackIds) == 0 {
		return nil, nil
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	var added []domain.TrackId
	err := r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, addTracksSQL, playlistId.UUID(), trackIdUUIDs(trackIds))
		if err != nil {
			return err
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

// RemoveTrack deletes the membership row and shifts only the tail behind it
// (position > the removed slot) up by one, so the cost is bounded by the rows
// after the track rather than the whole playlist. It reports whether the track
// was a member.
func (r *PgxPlaylistRepository) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (bool, error) {
	ctx, cancel := withDBTimeout(ctx)
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
			return err
		}
		removed = true
		_, err = tx.Exec(ctx,
			`UPDATE playlist_tracks SET position = position - 1 WHERE playlist_id = $1 AND position > $2`,
			playlistId.UUID(), position,
		)
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// RemoveTracks deletes every listed membership row and closes the gaps in one
// statement that touches only rows behind the first removed slot: each survivor
// moves up by the number of removed slots below it, which width_bucket counts by
// binary search over the sorted removed positions. It returns the removed ids in
// request order. An empty list still answers ErrPlaylistNotOwned for a playlist
// the caller does not own.
func (r *PgxPlaylistRepository) RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error) {
	ctx, cancel := withDBTimeout(ctx)
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
		return err
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// deleteMemberships deletes the listed tracks from the playlist and returns the
// set of ids it deleted plus their former positions, sorted ascending.
func deleteMemberships(ctx context.Context, tx pgx.Tx, playlistId domain.PlaylistId, trackIds []domain.TrackId) (map[uuid.UUID]bool, []int, error) {
	rows, err := tx.Query(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = ANY($2) RETURNING track_id, position`,
		playlistId.UUID(), trackIdUUIDs(trackIds),
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	gone := make(map[uuid.UUID]bool)
	var positions []int
	for rows.Next() {
		var (
			id       uuid.UUID
			position int
		)
		if err := rows.Scan(&id, &position); err != nil {
			return nil, nil, err
		}
		gone[id] = true
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.Ints(positions)
	return gone, positions, nil
}

// ReorderTracks writes the new positions in one set-based statement, inside the
// same owner-scoped playlist lock as the other membership writes, rewriting
// only the rows whose position actually changes. The plan comes from a read
// taken before the lock, so the membership is re-read under it: writing a plan
// an add or remove has since invalidated would tie two tracks at one position
// (the deferred unique constraint then aborts the commit) or leave the slot of
// a removed track empty.
func (r *PgxPlaylistRepository) ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(tracks))
	positions := make([]int, len(tracks))
	for i, t := range tracks {
		ids[i] = t.TrackId.UUID()
		positions[i] = t.Position
	}

	ctx, cancel := withDBTimeout(ctx)
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
		return err
	})
}

// requirePlanCoversMembership reads the playlist's members inside the caller's
// locked transaction and refuses a plan that is not exactly that set. It reads
// the same window GetTrackOrder served the caller — same order, same bound — so
// a playlist longer than the cap compares its first maxPlaylistTracks rows
// against the plan built from those same rows, rather than refusing every
// reorder of an over-cap playlist. FOR UPDATE, because deleting a track
// cascades into playlist_tracks without taking the playlist lock: locking the
// rows holds the membership still from this read to the write below.
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
		return err
	}
	members, err := collectUUIDSet(rows)
	if err != nil {
		return err
	}
	if !maps.Equal(uuidSet(planned), members) {
		return ports.ErrPlaylistChangedDuringReorder
	}
	return nil
}

func uuidSet(ids []uuid.UUID) map[uuid.UUID]bool {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func trackIdUUIDs(ids []domain.TrackId) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = id.UUID()
	}
	return out
}

// collectUUIDSet drains rows of a single uuid column into a set.
func collectUUIDSet(rows pgx.Rows) (map[uuid.UUID]bool, error) {
	defer rows.Close()
	set := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// inRequestOrder returns the ids of requested that are in set, in request order
// and without repeats.
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

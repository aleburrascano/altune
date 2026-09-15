package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (r *PgxPlaylistRepository) ListForUser(ctx context.Context, userId shared.UserId) ([]domain.PlaylistWithSummary, error) {
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
		ORDER BY p.created_at DESC`,
		userId.UUID(), domain.PreviewArtworkLimit,
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

// maxPlaylistTracks bounds the rows returned by GetWithTracks at the catalog
// module's read cap (domain.MaxLibraryPageSize). The only
// other cap, MaxPlaylistBatchSize, limits a single batch-add, not total size.
// It is a var so tests can exercise the bound without inserting the full cap.
var maxPlaylistTracks = domain.MaxLibraryPageSize

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

	_, err := r.pool.Exec(ctx,
		`UPDATE playlists SET name = $3, updated_at = $4 WHERE id = $1 AND user_id = $2`,
		playlist.ID.UUID(), playlist.UserId.UUID(), playlist.Name, playlist.UpdatedAt,
	)
	return err
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

// AddTrack appends the track at the authoritative max(position)+1 computed under
// a playlist lock. The caller-supplied position is advisory only: deriving the
// slot inside the locked transaction is what closes the concurrent-add race, so
// two simultaneous appends can never both land on the same position.
func (r *PgxPlaylistRepository) AddTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId, _ int) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO playlist_tracks (playlist_id, track_id, position)
			VALUES ($1, $2, COALESCE((SELECT MAX(position) + 1 FROM playlist_tracks WHERE playlist_id = $1), 0))`,
			playlistId.UUID(), trackId.UUID(),
		)
		return err
	})
}

// AddTracks appends the given tracks in order, each at the authoritative
// max(position)+1 computed under a playlist lock. Like AddTrack, the positions
// carried on the input are advisory: a single set-based insert assigns the run
// of slots atomically from the locked snapshot, so a concurrent add cannot wedge
// a duplicate position between them.
func (r *PgxPlaylistRepository) AddTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if len(tracks) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(tracks))
	for i, t := range tracks {
		ids[i] = t.TrackId.UUID()
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO playlist_tracks (playlist_id, track_id, position)
			SELECT $1, t.track_id, base.max_pos + t.ord
			FROM unnest($2::uuid[]) WITH ORDINALITY AS t(track_id, ord)
			CROSS JOIN (
				SELECT COALESCE(MAX(position), -1) AS max_pos
				FROM playlist_tracks WHERE playlist_id = $1
			) base`,
			playlistId.UUID(), ids,
		)
		return err
	})
}

func (r *PgxPlaylistRepository) RemoveTrack(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = $2`,
			playlistId.UUID(), trackId.UUID(),
		); err != nil {
			return err
		}
		return renumberPlaylistPositions(ctx, tx, playlistId.UUID())
	})
}

func (r *PgxPlaylistRepository) RemoveTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) error {
	if len(trackIds) == 0 {
		return nil
	}

	uuids := make([]uuid.UUID, len(trackIds))
	for i, id := range trackIds {
		uuids[i] = id.UUID()
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = ANY($2)`,
			playlistId.UUID(), uuids,
		); err != nil {
			return err
		}
		return renumberPlaylistPositions(ctx, tx, playlistId.UUID())
	})
}

func renumberPlaylistPositions(ctx context.Context, tx pgx.Tx, playlistId uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE playlist_tracks SET position = sub.new_pos
		FROM (
			SELECT track_id, ROW_NUMBER() OVER (ORDER BY position) - 1 AS new_pos
			FROM playlist_tracks WHERE playlist_id = $1
		) sub
		WHERE playlist_tracks.playlist_id = $1 AND playlist_tracks.track_id = sub.track_id`,
		playlistId,
	)
	return err
}

// ReorderTracks writes each track's new position in one batch, inside the same
// owner-scoped playlist lock as the other membership writes.
func (r *PgxPlaylistRepository) ReorderTracks(ctx context.Context, userId shared.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error {
	if len(tracks) == 0 {
		return nil
	}

	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return r.withOwnedPlaylistLock(ctx, playlistId, userId, func(tx pgx.Tx) error {
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
				_ = br.Close()
				return err
			}
		}
		return br.Close()
	})
}

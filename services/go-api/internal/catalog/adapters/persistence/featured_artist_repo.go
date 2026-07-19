package persistence

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// querier is the subset of pgxpool.Pool / pgx.Tx used for reads, so featured-artist
// loading works both on the pool and inside a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// writeTrackFeatured upserts each featured artist into the canonical
// featured_artists table (deduped on the generated identity_key) and links it to
// the track in position order. Must run inside a transaction. It does not clear
// existing links — callers replacing a set (backfill) delete first.
func writeTrackFeatured(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	trackID uuid.UUID,
	feats []domain.FeaturedArtist,
) error {
	for i, fa := range feats {
		var faID uuid.UUID
		err := tx.QueryRow(ctx,
			`INSERT INTO featured_artists (user_id, mbid, deezer_id, name, norm_name)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (user_id, identity_key) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`,
			userID, nullString(fa.MBID), nullInt64(fa.DeezerID), fa.Name, fa.NormalizedName(),
		).Scan(&faID)
		if err != nil {
			return fmt.Errorf("upsert featured artist %q: %w", fa.Name, err)
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO track_featured_artists (track_id, featured_artist_id, position)
			VALUES ($1,$2,$3)
			ON CONFLICT (track_id, featured_artist_id) DO NOTHING`,
			trackID, faID, i,
		)
		if err != nil {
			return fmt.Errorf("link featured artist %q: %w", fa.Name, err)
		}
	}
	return nil
}

// loadFeaturedForTracks batch-loads the featured artists for the given tracks in
// one query and attaches them (position-ordered). Avoids an N+1 across a list.
func loadFeaturedForTracks(ctx context.Context, q querier, tracks []*domain.Track) error {
	if len(tracks) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(tracks))
	byID := make(map[uuid.UUID]*domain.Track, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID.UUID()
		byID[t.ID.UUID()] = t
	}

	rows, err := q.Query(ctx,
		`SELECT tfa.track_id, fa.name, fa.mbid, fa.deezer_id
		FROM track_featured_artists tfa
		JOIN featured_artists fa ON fa.id = tfa.featured_artist_id
		WHERE tfa.track_id = ANY($1)
		ORDER BY tfa.track_id, tfa.position`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("load featured artists: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var trackID uuid.UUID
		var name string
		var mbid *string
		var deezerID *int64
		if err := rows.Scan(&trackID, &name, &mbid, &deezerID); err != nil {
			return fmt.Errorf("scan featured artist: %w", err)
		}
		t := byID[trackID]
		if t == nil {
			continue
		}
		fa := domain.FeaturedArtist{Name: name, Role: domain.RoleFeatured}
		if mbid != nil {
			fa.MBID = *mbid
		}
		if deezerID != nil {
			fa.DeezerID = *deezerID
		}
		t.FeaturedArtists = append(t.FeaturedArtists, fa)
	}
	return rows.Err()
}

// ReplaceFeaturedArtists deletes the track's existing featured-artist links and
// writes the new set in one transaction. Verifies the track belongs to the user
// first so the backfill can't cross tenants.
func (r *PgxTrackRepository) ReplaceFeaturedArtists(
	ctx context.Context,
	id domain.TrackId,
	userId shared.UserId,
	feats []domain.FeaturedArtist,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var owned bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1 AND user_id = $2)`,
		id.UUID(), userId.UUID(),
	).Scan(&owned)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("track %s not found for user", id.String())
	}

	if _, err := tx.Exec(ctx, `DELETE FROM track_featured_artists WHERE track_id = $1`, id.UUID()); err != nil {
		return fmt.Errorf("clear featured artists: %w", err)
	}
	if err := writeTrackFeatured(ctx, tx, userId.UUID(), id.UUID(), feats); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListTracksFeaturing returns the user's tracks crediting the featured artist,
// matched on its identity key. Ordered newest-first.
func (r *PgxTrackRepository) ListTracksFeaturing(
	ctx context.Context,
	userId shared.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+trackColumnsPrefixed+`
		FROM tracks t
		JOIN track_featured_artists tfa ON tfa.track_id = t.id
		JOIN featured_artists fa ON fa.id = tfa.featured_artist_id
		WHERE t.user_id = $1 AND fa.identity_key = $2
		ORDER BY t.added_at DESC, t.id DESC`,
		userId.UUID(), fa.IdentityKey(),
	)
	if err != nil {
		return nil, fmt.Errorf("list tracks featuring: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

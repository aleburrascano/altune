package persistence

import (
	"context"
	"fmt"

	"altune/go-api/internal/catalog/domain"

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

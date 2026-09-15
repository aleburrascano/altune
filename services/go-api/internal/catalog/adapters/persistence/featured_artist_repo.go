package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PgxFeaturedArtistRepository struct {
	pool pgxPool
}

func NewPgxFeaturedArtistRepository(pool *pgxpool.Pool) *PgxFeaturedArtistRepository {
	return &PgxFeaturedArtistRepository{pool: pool}
}

func writeTrackFeatured(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	trackID uuid.UUID,
	feats []domain.FeaturedArtist,
) error {
	for i, fa := range feats {
		faID, err := upsertFeaturedArtist(ctx, tx, userID, fa)
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

// upsertFeaturedArtist returns the id of the user's featured_artists row for
// fa, inserting it if absent. When NFKC changes the name key, a row persisted
// before NormalizedName applied NFKC may still carry the legacy key; that row
// is reused (preferring one already on the current key) so upgrading never
// forks an existing artist into a second row.
func upsertFeaturedArtist(ctx context.Context, tx pgx.Tx, userID uuid.UUID, fa domain.FeaturedArtist) (uuid.UUID, error) {
	var faID uuid.UUID
	if keys := featuredIdentityKeys(fa); len(keys) > 1 {
		err := tx.QueryRow(ctx,
			`UPDATE featured_artists SET name = $3
			WHERE id = (
				SELECT id FROM featured_artists
				WHERE user_id = $1 AND identity_key = ANY($2)
				ORDER BY identity_key = $4 DESC
				LIMIT 1
			)
			RETURNING id`,
			userID, keys, fa.Name, fa.IdentityKey(),
		).Scan(&faID)
		if err == nil {
			return faID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	err := tx.QueryRow(ctx,
		`INSERT INTO featured_artists (user_id, mbid, deezer_id, name, norm_name)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, identity_key) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`,
		userID, nullString(fa.MBID), nullInt64(fa.DeezerID), fa.Name, fa.NormalizedName(),
	).Scan(&faID)
	return faID, err
}

// featuredIdentityKeys is the set of identity_key values a stored row for fa
// may carry: the current key, plus the pre-NFKC legacy key when it differs.
func featuredIdentityKeys(fa domain.FeaturedArtist) []string {
	key, legacy := fa.IdentityKey(), fa.LegacyIdentityKey()
	if key == legacy {
		return []string{key}
	}
	return []string{key, legacy}
}

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

func (r *PgxFeaturedArtistRepository) ReplaceFeaturedArtists(
	ctx context.Context,
	id domain.TrackId,
	userId shared.UserId,
	feats []domain.FeaturedArtist,
) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

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

// featuringResultCap bounds ListTracksFeaturing at the catalog module's read cap
// (domain.MaxLibraryPageSize), like every other list path. Without it a featured
// artist (or name) matching tens of thousands of tracks materializes the entire
// result set in memory and serializes it in one response.
const featuringResultCap = domain.MaxLibraryPageSize

// buildFeaturingQuery returns the SQL and args for ListTracksFeaturing with the
// result set bounded by featuringResultCap. Extracted so the cap is testable
// without a live database.
// identityKeys holds every key a matching row may carry (see
// featuredIdentityKeys). DISTINCT keeps a track linked to both a legacy-key and
// a current-key row from appearing twice.
func buildFeaturingQuery(userID uuid.UUID, identityKeys []string) (string, []any) {
	sql := `SELECT ` + trackColumnsPrefixed + `
		FROM tracks t
		WHERE t.user_id = $1 AND EXISTS (
			SELECT 1 FROM track_featured_artists tfa
			JOIN featured_artists fa ON fa.id = tfa.featured_artist_id
			WHERE tfa.track_id = t.id AND fa.identity_key = ANY($2)
		)
		ORDER BY t.added_at DESC, t.id DESC
		LIMIT $3`
	return sql, []any{userID, identityKeys, featuringResultCap}
}

func (r *PgxFeaturedArtistRepository) ListTracksFeaturing(
	ctx context.Context,
	userId shared.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	sql, args := buildFeaturingQuery(userId.UUID(), featuredIdentityKeys(fa))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list tracks featuring: %w", err)
	}
	defer rows.Close()

	tracks, err := collectTracks(rows)
	if err != nil {
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

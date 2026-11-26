package persistence

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/database"
	sharedids "altune/go-api/internal/shared/ids"
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

type PostgresFeaturedArtistRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresFeaturedArtistRepository(pool *pgxpool.Pool) *PostgresFeaturedArtistRepository {
	return &PostgresFeaturedArtistRepository{pool: pool}
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
			return uuid.Nil, fmt.Errorf("upsert featured artist %q: %w", fa.Name, err)
		}
	}
	err := tx.QueryRow(ctx,
		`INSERT INTO featured_artists (user_id, mbid, deezer_id, name, norm_name)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, identity_key) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`,
		userID, nullString(fa.MBID), nullInt64(fa.DeezerID), fa.Name, fa.NormalizedName(),
	).Scan(&faID)
	if err != nil {
		return faID, fmt.Errorf("insert featured artist %q: %w", fa.Name, err)
	}
	return faID, nil
}

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
		`SELECT tfa.track_id, fa.name, COALESCE(fa.mbid, '') AS mbid, COALESCE(fa.deezer_id, 0) AS deezer_id
		FROM track_featured_artists tfa
		JOIN featured_artists fa ON fa.id = tfa.featured_artist_id
		WHERE tfa.track_id = ANY($1)
		ORDER BY tfa.track_id, tfa.position`,
		ids,
	)
	if err != nil {
		return fmt.Errorf("load featured artists: %w", err)
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByName[featuredArtistLink])
	if err != nil {
		return fmt.Errorf("scan featured artists: %w", err)
	}
	for _, link := range links {
		if t := byID[link.TrackID]; t != nil {
			t.FeaturedArtists = append(t.FeaturedArtists, link.featuredArtist())
		}
	}
	return nil
}

type featuredArtistLink struct {
	TrackID  uuid.UUID
	Name     string
	MBID     string
	DeezerID int64
}

func (l featuredArtistLink) featuredArtist() domain.FeaturedArtist {
	return domain.FeaturedArtist{Name: l.Name, Role: domain.RoleFeatured, MBID: l.MBID, DeezerID: l.DeezerID}
}

func (r *PostgresFeaturedArtistRepository) ReplaceFeaturedArtists(
	ctx context.Context,
	id domain.TrackId,
	userId sharedids.UserId,
	feats []domain.FeaturedArtist,
) error {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	return classifyDBError(r.replaceInTx(ctx, id, userId, feats))
}

func (r *PostgresFeaturedArtistRepository) replaceInTx(ctx context.Context, id domain.TrackId, userId sharedids.UserId, feats []domain.FeaturedArtist) error {
	return database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := requireOwnedTrack(ctx, tx, id, userId); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM track_featured_artists WHERE track_id = $1`, id.UUID()); err != nil {
			return fmt.Errorf("clear featured artists: %w", err)
		}
		return writeTrackFeatured(ctx, tx, userId.UUID(), id.UUID(), feats)
	})
}

func requireOwnedTrack(ctx context.Context, tx pgx.Tx, id domain.TrackId, userId sharedids.UserId) error {
	var owned bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tracks WHERE id = $1 AND user_id = $2)`,
		id.UUID(), userId.UUID(),
	).Scan(&owned)
	if err != nil {
		return fmt.Errorf("check that user %s owns track %s: %w", userId.UUID(), id.UUID(), err)
	}
	if !owned {
		return fmt.Errorf("track %s not found for user", id.String())
	}
	return nil
}

const featuringResultCap = domain.MaxLibraryPageSize

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

func (r *PostgresFeaturedArtistRepository) ListTracksFeaturing(
	ctx context.Context,
	userId sharedids.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	ctx, cancel := withDBTimeout(ctx)
	defer cancel()

	sql, args := buildFeaturingQuery(userId.UUID(), featuredIdentityKeys(fa))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list tracks featuring: %w", classifyDBError(err))
	}
	tracks, err := collectTracks(rows)
	if err != nil {
		return nil, classifyDBError(err)
	}
	if err := loadFeaturedForTracks(ctx, r.pool, tracks); err != nil {
		return nil, classifyDBError(err)
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

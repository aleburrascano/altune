package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.FavoritesRepository   = (*PgxFavoritesRepository)(nil)
	_ ports.DeletedIdentityEraser = (*PgxFavoritesRepository)(nil)
)

type PgxFavoritesRepository struct {
	pool *pgxpool.Pool
}

func NewPgxFavoritesRepository(pool *pgxpool.Pool) *PgxFavoritesRepository {
	return &PgxFavoritesRepository{pool: pool}
}

func (r *PgxFavoritesRepository) Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error {
	tag, err := r.pool.Exec(ctx, addFavoriteBelowCapSQL,
		userId.UUID(), fav.Kind.String(), fav.Key, fav.Title, fav.Subtitle, fav.ImageURL, ports.MaxFavoritesPerUser,
	)
	if err != nil {
		return fmt.Errorf("insert favorite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insert favorite: %w", ports.ErrFavoritesFull)
	}
	return nil
}

const addFavoriteBelowCapSQL = `
	INSERT INTO discovery_favorites (user_id, kind, entity_key, title, subtitle, image_url)
	SELECT $1::uuid, $2::text, $3::text, $4::text, $5::text, $6::text
	WHERE (SELECT count(*) FROM discovery_favorites WHERE user_id = $1::uuid) < $7::int
	   OR EXISTS (
		SELECT 1 FROM discovery_favorites
		WHERE user_id = $1::uuid AND kind = $2::text AND entity_key = $3::text)
	ON CONFLICT (user_id, kind, entity_key) DO UPDATE
	SET title = EXCLUDED.title, subtitle = EXCLUDED.subtitle, image_url = EXCLUDED.image_url`

func (r *PgxFavoritesRepository) Remove(ctx context.Context, userId shared.UserId, kind domain.ResultKind, key string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM discovery_favorites WHERE user_id = $1 AND kind = $2 AND entity_key = $3`,
		userId.UUID(), kind.String(), key,
	)
	if err != nil {
		return fmt.Errorf("delete favorite: %w", err)
	}
	return nil
}

// eraseFavoritesOfDeletedIdentitiesSQL drops the favorites of accounts whose
// identity is gone. Favorites have no retention at all — Remove is the only
// delete, and it needs the owner to ask — so this is the single path by which a
// deleted account's saved artists and albums ever leave the table.
//
// Cost: one pass over discovery_favorites per run, each row probing auth.users'
// primary key.
const eraseFavoritesOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_favorites f
	WHERE EXISTS (SELECT 1 FROM auth.users)
	  AND f.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.id = f.user_id)`

func (r *PgxFavoritesRepository) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	return eraseRowsOfDeletedIdentities(ctx, r.pool,
		"erase favorites of deleted identities", eraseFavoritesOfDeletedIdentitiesSQL)
}

func (r *PgxFavoritesRepository) ListForUser(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT kind, entity_key, title, subtitle, image_url, created_at
		FROM discovery_favorites WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userId.UUID(), ports.MaxFavoritesPerUser,
	)
	if err != nil {
		return nil, fmt.Errorf("query favorites: %w", err)
	}
	defer rows.Close()

	return collectRows(rows, func(rows pgx.Rows) (domain.Favorite, error) {
		var (
			kind      string
			key       string
			title     string
			subtitle  string
			imageURL  string
			createdAt time.Time
		)
		if err := rows.Scan(&kind, &key, &title, &subtitle, &imageURL, &createdAt); err != nil {
			return domain.Favorite{}, fmt.Errorf("scan favorite: %w", err)
		}
		parsed, err := domain.ParseResultKind(kind)
		if err != nil {
			return domain.Favorite{}, errSkipRow
		}
		return domain.Favorite{
			Kind:      parsed,
			Key:       key,
			Title:     title,
			Subtitle:  subtitle,
			ImageURL:  imageURL,
			CreatedAt: createdAt,
		}, nil
	})
}

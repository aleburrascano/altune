package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/core/ports"
	shared "altune/go-api/internal/shared/kernel"
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

const eraseFavoritesOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_favorites f
	WHERE EXISTS (SELECT 1 FROM auth_user_ids)
	  AND f.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth_user_ids u WHERE u.id = f.user_id)`

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
	stored, err := pgx.CollectRows(rows, pgx.RowToStructByName[storedFavorite])
	if err != nil {
		return nil, fmt.Errorf("scan favorites: %w", err)
	}
	favorites := []domain.Favorite{}
	for _, f := range stored {
		if favorite, known := f.favorite(); known {
			favorites = append(favorites, favorite)
		}
	}
	return favorites, nil
}

type storedFavorite struct {
	Kind      string
	EntityKey string
	Title     string
	Subtitle  string
	ImageURL  string
	CreatedAt time.Time
}

func (f storedFavorite) favorite() (domain.Favorite, bool) {
	kind, err := domain.ParseResultKind(f.Kind)
	if err != nil {
		return domain.Favorite{}, false
	}
	return domain.Favorite{
		Kind:      kind,
		Key:       f.EntityKey,
		Title:     f.Title,
		Subtitle:  f.Subtitle,
		ImageURL:  f.ImageURL,
		CreatedAt: f.CreatedAt,
	}, true
}

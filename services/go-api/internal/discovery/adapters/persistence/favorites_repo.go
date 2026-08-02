package persistence

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.FavoritesRepository = (*PgxFavoritesRepository)(nil)

type PgxFavoritesRepository struct {
	pool *pgxpool.Pool
}

func NewPgxFavoritesRepository(pool *pgxpool.Pool) *PgxFavoritesRepository {
	return &PgxFavoritesRepository{pool: pool}
}

func (r *PgxFavoritesRepository) Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO discovery_favorites (user_id, kind, entity_key, title, subtitle, image_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, kind, entity_key) DO UPDATE
		SET title = EXCLUDED.title, subtitle = EXCLUDED.subtitle, image_url = EXCLUDED.image_url`,
		userId.UUID(), fav.Kind.String(), fav.Key, fav.Title, fav.Subtitle, fav.ImageURL,
	)
	if err != nil {
		return fmt.Errorf("insert favorite: %w", err)
	}
	return nil
}

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

func (r *PgxFavoritesRepository) ListForUser(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT kind, entity_key, title, subtitle, image_url, created_at
		FROM discovery_favorites WHERE user_id = $1 ORDER BY created_at DESC`,
		userId.UUID(),
	)
	if err != nil {
		return nil, fmt.Errorf("query favorites: %w", err)
	}
	defer rows.Close()

	favorites := []domain.Favorite{}
	for rows.Next() {
		var (
			kind      string
			key       string
			title     string
			subtitle  string
			imageURL  string
			createdAt time.Time
		)
		if err := rows.Scan(&kind, &key, &title, &subtitle, &imageURL, &createdAt); err != nil {
			return nil, fmt.Errorf("scan favorite: %w", err)
		}
		parsed, err := domain.ParseResultKind(kind)
		if err != nil {
			continue
		}
		favorites = append(favorites, domain.Favorite{
			Kind:      parsed,
			Key:       key,
			Title:     title,
			Subtitle:  subtitle,
			ImageURL:  imageURL,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate favorites: %w", err)
	}
	return favorites, nil
}

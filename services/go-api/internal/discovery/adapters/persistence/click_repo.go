package persistence

import (
	"context"
	"errors"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.SearchClickRepository = (*PgxSearchClickRepository)(nil)

type PgxSearchClickRepository struct {
	pool *pgxpool.Pool
}

func NewPgxSearchClickRepository(pool *pgxpool.Pool) *PgxSearchClickRepository {
	return &PgxSearchClickRepository{pool: pool}
}

func (r *PgxSearchClickRepository) InsertIfOutsideWindow(ctx context.Context, click *domain.SearchClick, windowSeconds int) (bool, error) {
	threshold := time.Now().UTC().Add(-time.Duration(windowSeconds) * time.Second)

	var dedupedID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM discovery_search_clicks
		WHERE user_id = $1 AND query_norm = $2 AND result_signature = $3 AND clicked_at > $4
		ORDER BY clicked_at DESC LIMIT 1`,
		click.UserId.UUID(), click.QueryNorm, click.ResultSignature, threshold,
	).Scan(&dedupedID)

	if err == nil {
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO discovery_search_clicks (id, user_id, query_norm, result_signature, position, confidence, clicked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		click.ID, click.UserId.UUID(), click.QueryNorm, click.ResultSignature,
		click.Position, click.Confidence.String(), click.ClickedAt,
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

package persistence

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.SearchHistoryRepository = (*PgxSearchHistoryRepository)(nil)

type PgxSearchHistoryRepository struct {
	pool *pgxpool.Pool
}

func NewPgxSearchHistoryRepository(pool *pgxpool.Pool) *PgxSearchHistoryRepository {
	return &PgxSearchHistoryRepository{pool: pool}
}

func (r *PgxSearchHistoryRepository) Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO discovery_search_history (id, user_id, query, query_norm, executed_at, result_clicked_signature)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ID, entry.UserId.UUID(), entry.Query, entry.QueryNorm, entry.ExecutedAt, entry.ResultClickedSignature,
	)
	if err != nil {
		return fmt.Errorf("insert search history: %w", err)
	}
	return nil
}

func (r *PgxSearchHistoryRepository) TrimToN(ctx context.Context, userId shared.UserId, n int) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM discovery_search_history
		WHERE user_id = $1
		AND id NOT IN (
			SELECT id FROM discovery_search_history
			WHERE user_id = $1
			ORDER BY executed_at DESC, id DESC
			LIMIT $2
		)`,
		userId.UUID(), n,
	)
	if err != nil {
		return fmt.Errorf("trim search history: %w", err)
	}
	return nil
}

func (r *PgxSearchHistoryRepository) ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT h.id, h.user_id, h.query, h.query_norm, h.executed_at, h.result_clicked_signature
		FROM discovery_search_history h
		INNER JOIN (
			SELECT query_norm, MAX(executed_at) AS max_executed_at
			FROM discovery_search_history
			WHERE user_id = $1
			GROUP BY query_norm
			ORDER BY MAX(executed_at) DESC
			LIMIT $2
		) latest ON h.query_norm = latest.query_norm
			AND h.executed_at = latest.max_executed_at
			AND h.user_id = $1
		ORDER BY h.executed_at DESC, h.id DESC`,
		userId.UUID(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query search history: %w", err)
	}
	defer rows.Close()

	var entries []*domain.SearchHistoryEntry
	for rows.Next() {
		var (
			id        uuid.UUID
			uid       uuid.UUID
			query     string
			queryNorm string
			execAt    time.Time
			clickSig  *string
		)
		if err := rows.Scan(&id, &uid, &query, &queryNorm, &execAt, &clickSig); err != nil {
			return nil, fmt.Errorf("scan search history: %w", err)
		}
		entries = append(entries, &domain.SearchHistoryEntry{
			ID:                     id,
			UserId:                 shared.NewUserId(uid),
			Query:                  query,
			QueryNorm:              queryNorm,
			ExecutedAt:             execAt,
			ResultClickedSignature: clickSig,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search history: %w", err)
	}
	return entries, nil
}

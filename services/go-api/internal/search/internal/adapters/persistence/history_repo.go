package persistence

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.HistoryWriter         = (*PostgresSearchHistoryRepository)(nil)
	_ ports.HistoryReader         = (*PostgresSearchHistoryRepository)(nil)
	_ ports.HistoryEraser         = (*PostgresSearchHistoryRepository)(nil)
	_ ports.DeletedUserDataEraser = (*PostgresSearchHistoryRepository)(nil)
)

const historyResendWindow = time.Minute

func eraseRowsOfDeletedIdentities(ctx context.Context, pool *pgxpool.Pool, op, deleteSQL string) (int64, error) {
	tag, err := pool.Exec(ctx, deleteSQL, ids.SystemUserId().UUID())
	if err != nil {
		return 0, database.WrapUnreadable(op, ports.ErrIdentityStoreUnavailable, err)
	}
	return tag.RowsAffected(), nil
}

type PostgresSearchHistoryRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSearchHistoryRepository(pool *pgxpool.Pool) *PostgresSearchHistoryRepository {
	return &PostgresSearchHistoryRepository{pool: pool}
}

const insertHistoryUnlessResentSQL = `INSERT INTO discovery_search_history (id, user_id, query, query_norm, executed_at, result_clicked_signature)
	SELECT $1, $2, $3, $4, $5, $6
	WHERE NOT EXISTS (
		SELECT 1 FROM discovery_search_history_cleared c
		WHERE c.user_id = $2 AND c.cleared_at > $5
	)
	AND NOT EXISTS (
		SELECT 1 FROM (
			SELECT h.query_norm, h.executed_at FROM discovery_search_history h
			WHERE h.user_id = $2
			ORDER BY h.executed_at DESC, h.id DESC
			LIMIT 1
		) latest
		WHERE latest.query_norm = $4 AND latest.executed_at > $7
	)`

func (r *PostgresSearchHistoryRepository) Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error {
	err := execUnderLocks(ctx, r.pool, entry.UserId.UUID(), historyInsertLocks, insertHistoryUnlessResentSQL,
		entry.ID.UUID(), entry.UserId.UUID(), entry.Query, entry.QueryNorm, entry.ExecutedAt, entry.ResultClickedSignature,
		entry.ExecutedAt.Add(-historyResendWindow),
	)
	if err != nil {
		return fmt.Errorf("insert search history: %w", err)
	}
	return nil
}

func (r *PostgresSearchHistoryRepository) TrimToN(ctx context.Context, userId ids.UserId, n int) error {
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

const lockSearchTextOfUserSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text, 0))`

const shareSearchTextLockOfUserSQL = `SELECT pg_advisory_xact_lock_shared(hashtextextended($1::uuid::text, 0))`

const lockHistoryInsertsOfUserSQL = `SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text, 1))`

var historyInsertLocks = []string{shareSearchTextLockOfUserSQL, lockHistoryInsertsOfUserSQL}

func execUnderSearchTextLock(ctx context.Context, pool *pgxpool.Pool, userId uuid.UUID, sql string, args ...any) error {
	return execUnderLocks(ctx, pool, userId, []string{shareSearchTextLockOfUserSQL}, sql, args...)
}

func execUnderLocks(ctx context.Context, pool *pgxpool.Pool, userId uuid.UUID, locks []string, sql string, args ...any) error {
	if err := database.InTx(ctx, pool, func(tx pgx.Tx) error {
		for _, lock := range locks {
			if _, err := tx.Exec(ctx, lock, userId); err != nil {
				return fmt.Errorf("lock search history of user %s: %w", userId, err)
			}
		}
		_, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("write search history of user %s: %w", userId, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("run transaction: %w", err)
	}
	return nil
}

const stampHistoryClearedSQL = `INSERT INTO discovery_search_history_cleared (user_id, cleared_at)
	VALUES ($1, clock_timestamp())
	ON CONFLICT (user_id) DO UPDATE SET cleared_at = EXCLUDED.cleared_at`

var eraseSearchTextOfUserSQL = []string{
	lockSearchTextOfUserSQL,
	stampHistoryClearedSQL,
	`DELETE FROM discovery_search_history WHERE user_id = $1`,
	eraseEventSearchTextOfUserSQL,
}

func (r *PostgresSearchHistoryRepository) EraseSearchTextForUser(ctx context.Context, userId ids.UserId) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		for _, eraseSQL := range eraseSearchTextOfUserSQL {
			if _, err := tx.Exec(ctx, eraseSQL, userId.UUID()); err != nil {
				return fmt.Errorf("erase search text of user %s: %w", userId.UUID(), err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("erase search text: %w", err)
	}
	return nil
}

const eraseHistoryOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_search_history h
	WHERE EXISTS (SELECT 1 FROM auth_user_ids)
	  AND h.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth_user_ids u WHERE u.id = h.user_id)`

func (r *PostgresSearchHistoryRepository) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	return eraseRowsOfDeletedIdentities(ctx, r.pool,
		"erase search history of deleted identities", eraseHistoryOfDeletedIdentitiesSQL)
}

func (r *PostgresSearchHistoryRepository) ListDistinctRecent(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
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
	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.SearchHistoryEntry, error) {
		var (
			id        uuid.UUID
			uid       uuid.UUID
			query     string
			queryNorm string
			execAt    time.Time
			clickSig  *string
		)
		if err := row.Scan(&id, &uid, &query, &queryNorm, &execAt, &clickSig); err != nil {
			return nil, fmt.Errorf("scan search history: %w", err)
		}
		return &domain.SearchHistoryEntry{
			ID:                     ids.EntityIdFromBytes(id),
			UserId:                 ids.NewUserId(uid),
			Query:                  query,
			QueryNorm:              queryNorm,
			ExecutedAt:             execAt,
			ResultClickedSignature: clickSig,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect search history: %w", err)
	}
	return collected, nil
}

package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.HistoryWriter         = (*PgxSearchHistoryRepository)(nil)
	_ ports.HistoryReader         = (*PgxSearchHistoryRepository)(nil)
	_ ports.HistoryEraser         = (*PgxSearchHistoryRepository)(nil)
	_ ports.DeletedIdentityEraser = (*PgxSearchHistoryRepository)(nil)
)

// Postgres SQLSTATEs that mean the identity store is not readable from here,
// rather than that an owner's account was deleted. Both are measured against
// Postgres 16: a read through a missing schema reports the relation undefined
// (42P01, not 3F000), and a role without the grant reports 42501.
const (
	undefinedTableCode   = "42P01"
	insufficientPrivCode = "42501"
)

// eraseRowsOfDeletedIdentities runs one discovery table's anti-join delete
// against the identity store, reporting the rows it removed and classifying a
// store this deployment cannot read so the sweep idles instead of erasing.
//
// Every erase*OfDeletedIdentitiesSQL opens with `EXISTS (SELECT 1 FROM
// auth.users)`, which is the blast bound on a delete that cannot be undone: a
// role that reaches auth.users but sees none of its rows (row-level security, a
// restore still in flight) would otherwise report every owner as deleted and
// empty the table. It costs one index probe.
//
// $1 is the one owner that is absent from auth.users by design rather than by
// deletion: shared.SystemUserId stamps the server-emitted discography_observed
// rows and the smoke eval's writes, so to an anti-join the synthetic account
// looks deleted and an unguarded sweep would evict the discography quality
// aggregate on its first tick. It is supplied here rather than per table so a
// table joining the sweep cannot omit it quietly — a delete whose SQL drops the
// `<> $1` clause takes no parameters and fails on its first run.
//
// The deletes carry no LIMIT, matching the retention prunes over the same tables
// (pruneEventsByTypeSQL): each run re-evaluates the whole tail, so a missed run
// defers eviction without skipping a row, and after the first run only the
// accounts deleted since it are left to find.
func eraseRowsOfDeletedIdentities(ctx context.Context, pool *pgxpool.Pool, op, deleteSQL string) (int64, error) {
	tag, err := pool.Exec(ctx, deleteSQL, shared.SystemUserId().UUID())
	if err != nil {
		return 0, identityStoreErr(op, err)
	}
	return tag.RowsAffected(), nil
}

func identityStoreErr(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && isIdentityStoreUnreadable(pgErr.Code) {
		return fmt.Errorf("%s: %w: %w", op, ports.ErrIdentityStoreUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func isIdentityStoreUnreadable(sqlState string) bool {
	switch sqlState {
	case undefinedTableCode, insufficientPrivCode:
		return true
	}
	return false
}

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

// eraseSearchTextOfUserSQL is every store that keeps what one account searched
// for, in the order one transaction applies them. A statement joining this list
// takes the owner as $1 and must be safe to re-run: clearing an already-cleared
// account is a no-op, not an error.
var eraseSearchTextOfUserSQL = []string{
	`DELETE FROM discovery_search_history WHERE user_id = $1`,
	eraseEventSearchTextOfUserSQL,
}

func (r *PgxSearchHistoryRepository) EraseSearchTextForUser(ctx context.Context, userId shared.UserId) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin erase search text: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, eraseSQL := range eraseSearchTextOfUserSQL {
		if _, err := tx.Exec(ctx, eraseSQL, userId.UUID()); err != nil {
			return fmt.Errorf("erase search text: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// eraseHistoryOfDeletedIdentitiesSQL drops the search history of accounts whose
// identity is gone. TrimToN keeps history at 100 rows per user but has no age
// limit, so without this an account nobody can log into any more keeps its
// free-text queries forever.
//
// Cost: one pass over discovery_search_history per run, each row probing
// auth.users' primary key.
const eraseHistoryOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_search_history h
	WHERE EXISTS (SELECT 1 FROM auth.users)
	  AND h.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.id = h.user_id)`

func (r *PgxSearchHistoryRepository) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	return eraseRowsOfDeletedIdentities(ctx, r.pool,
		"erase search history of deleted identities", eraseHistoryOfDeletedIdentitiesSQL)
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

	return collectRows(rows, func(rows pgx.Rows) (*domain.SearchHistoryEntry, error) {
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
		return &domain.SearchHistoryEntry{
			ID:                     id,
			UserId:                 shared.NewUserId(uid),
			Query:                  query,
			QueryNorm:              queryNorm,
			ExecutedAt:             execAt,
			ResultClickedSignature: clickSig,
		}, nil
	})
}

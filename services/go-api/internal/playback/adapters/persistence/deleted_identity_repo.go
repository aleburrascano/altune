package persistence

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres SQLSTATEs that mean the identity store is not readable from here,
// rather than that an owner's account was deleted. Both are measured against
// Postgres 16: a SELECT through a missing schema reports the relation undefined
// (42P01, not 3F000), and a role without the grant reports 42501.
const (
	undefinedTableCode   = "42P01"
	insufficientPrivCode = "42501"
)

var _ ports.DeletedIdentityLister = (*PgxDeletedIdentityRepository)(nil)

type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PgxDeletedIdentityRepository reads stored queue state against Supabase's
// auth.users, which lives in this same database (DATABASE_URL is the Supabase
// pooler) but is owned by Supabase Auth: it is the only in-database record of
// which accounts still exist.
type PgxDeletedIdentityRepository struct {
	pool rowsQuerier
}

func NewPgxDeletedIdentityRepository(pool *pgxpool.Pool) *PgxDeletedIdentityRepository {
	return &PgxDeletedIdentityRepository{pool: pool}
}

// ownersWithoutIdentitySQL anti-joins stored queue state against the identity
// store. Gone means the row is gone: an account Supabase soft-deletes keeps its
// auth.users row, and its queue state is left for the self-service erasure
// rather than read off a column of Supabase's own schema.
//
// `EXISTS (SELECT 1 FROM auth.users)` is the blast bound on an erasure
// that cannot be undone: a role that reaches the table but sees none of its rows
// (row-level security, a restore still in flight) would otherwise report every
// owner as deleted and erase every stored queue. It costs one index probe.
//
// Cost: one pass over playback_queue_state per run — one row per user with a
// stored queue — each row probing auth.users' primary key.
const ownersWithoutIdentitySQL = `
	SELECT q.user_id
	FROM playback_queue_state q
	WHERE EXISTS (SELECT 1 FROM auth.users)
	  AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.id = q.user_id)
	ORDER BY q.updated_at
	LIMIT $1`

func (r *PgxDeletedIdentityRepository) ListOwnersWithoutIdentity(ctx context.Context, limit int) ([]shared.UserId, error) {
	ctx, cancel := context.WithTimeout(ctx, queueStateOpTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, ownersWithoutIdentitySQL, limit)
	if err != nil {
		return nil, identityStoreErr(err)
	}
	defer rows.Close()
	return scanOwners(rows)
}

func scanOwners(rows pgx.Rows) ([]shared.UserId, error) {
	var owners []shared.UserId
	for rows.Next() {
		var owner uuid.UUID
		if err := rows.Scan(&owner); err != nil {
			return nil, fmt.Errorf("scan queue state owner: %w", err)
		}
		owners = append(owners, shared.NewUserId(owner))
	}
	if err := rows.Err(); err != nil {
		return nil, identityStoreErr(err)
	}
	return owners, nil
}

// identityStoreErr classifies a failed read of the identity store: an absent or
// forbidden auth.users is the unavailable sentinel its caller idles on, and
// anything else is a plain failure the caller retries next run.
func identityStoreErr(err error) error {
	const op = "list owners without identity"
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

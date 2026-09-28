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

const (
	undefinedTableCode   = "42P01"
	insufficientPrivCode = "42501"
)

var _ ports.DeletedIdentityLister = (*PgxDeletedIdentityRepository)(nil)

type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PgxDeletedIdentityRepository struct {
	pool rowsQuerier
}

func NewPgxDeletedIdentityRepository(pool *pgxpool.Pool) *PgxDeletedIdentityRepository {
	return &PgxDeletedIdentityRepository{pool: pool}
}

const ownersWithoutIdentitySQL = `
	SELECT q.user_id
	FROM playback_queue_state q
	WHERE q.erased_at IS NULL
	  AND EXISTS (SELECT 1 FROM auth.users)
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

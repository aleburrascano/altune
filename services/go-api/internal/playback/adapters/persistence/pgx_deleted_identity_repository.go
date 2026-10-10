package persistence

import (
	"altune/go-api/internal/playback/ports"
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

var _ ports.DeletedIdentityLister = (*PgxDeletedIdentityRepository)(nil)

const DefaultOwnerScanPageTimeout = 5 * time.Second

type rowsQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PgxDeletedIdentityRepository struct {
	pool    rowsQuerier
	timeout time.Duration
}

func NewPgxDeletedIdentityRepository(pool *pgxpool.Pool, opts ...func(*PgxDeletedIdentityRepository)) *PgxDeletedIdentityRepository {
	r := &PgxDeletedIdentityRepository{pool: pool, timeout: DefaultOwnerScanPageTimeout}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithOwnerScanPageTimeout(d time.Duration) func(*PgxDeletedIdentityRepository) {
	return func(r *PgxDeletedIdentityRepository) {
		r.timeout = d
	}
}

func (r *PgxDeletedIdentityRepository) opTimeout() time.Duration {
	if r.timeout > 0 {
		return r.timeout
	}
	return DefaultOwnerScanPageTimeout
}

const ownersWithoutIdentitySQL = `
	WITH page AS (
	  SELECT q.user_id
	  FROM playback_queue_state q
	  WHERE q.user_id > $1
	    AND q.erased_at IS NULL
	  ORDER BY q.user_id
	  LIMIT $2
	)
	SELECT p.user_id, u.id IS NULL
	FROM page p
	LEFT JOIN LATERAL (SELECT a.id FROM auth.users a WHERE a.id = p.user_id) u ON true
	WHERE EXISTS (SELECT 1 FROM auth.users)
	ORDER BY p.user_id`

func (r *PgxDeletedIdentityRepository) ListOwnersWithoutIdentity(ctx context.Context, after shared.UserId, window int) (ports.OwnerScanPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.opTimeout())
	defer cancel()

	rows, err := r.pool.Query(ctx, ownersWithoutIdentitySQL, after.UUID(), window)
	if err != nil {
		return ports.OwnerScanPage{}, identityStoreErr(err)
	}
	defer rows.Close()
	return scanOwnerPage(rows, after, window)
}

func scanOwnerPage(rows pgx.Rows, after shared.UserId, window int) (ports.OwnerScanPage, error) {
	page := ports.OwnerScanPage{Next: after}
	scanned := 0
	for rows.Next() {
		var owner uuid.UUID
		var orphaned bool
		if err := rows.Scan(&owner, &orphaned); err != nil {
			return ports.OwnerScanPage{}, fmt.Errorf("scan queue state owner: %w", err)
		}
		scanned++
		page.Next = shared.NewUserId(owner)
		if orphaned {
			page.Orphaned = append(page.Orphaned, page.Next)
		}
	}
	if err := rows.Err(); err != nil {
		return ports.OwnerScanPage{}, identityStoreErr(err)
	}
	page.Done = scanned < window
	return page, nil
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

package persistence

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/database"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ ports.DeletedIdentityLister = (*PgxDeletedIdentityRepository)(nil)

const DefaultOwnerScanPageTimeout = 5 * time.Second

type DeletedIdentityDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PgxDeletedIdentityRepository struct {
	pool DeletedIdentityDB
}

func NewPgxDeletedIdentityRepository(pool DeletedIdentityDB, opts ...func(*PgxDeletedIdentityRepository)) *PgxDeletedIdentityRepository {
	r := &PgxDeletedIdentityRepository{pool: pool}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

const listOwnersWithoutIdentityOp = "list owners without identity"

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
	ctx, cancel := context.WithTimeout(ctx, DefaultOwnerScanPageTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, ownersWithoutIdentitySQL, after.UUID(), window)
	if err != nil {
		return ports.OwnerScanPage{}, database.WrapUnreadable(listOwnersWithoutIdentityOp, ports.ErrIdentityStoreUnavailable, err)
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
		return ports.OwnerScanPage{}, database.WrapUnreadable(listOwnersWithoutIdentityOp, ports.ErrIdentityStoreUnavailable, err)
	}
	page.Done = scanned < window
	return page, nil
}

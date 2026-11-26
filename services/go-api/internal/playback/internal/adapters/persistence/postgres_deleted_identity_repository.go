package persistence

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/ids"
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ ports.DeletedUserDataLister = (*PostgresDeletedIdentityRepository)(nil)

const DefaultOwnerScanPageTimeout = 5 * time.Second

type DeletedUserDataDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PostgresDeletedIdentityRepository struct {
	pool DeletedUserDataDB
}

func NewPostgresDeletedIdentityRepository(pool DeletedUserDataDB, opts ...func(*PostgresDeletedIdentityRepository)) *PostgresDeletedIdentityRepository {
	r := &PostgresDeletedIdentityRepository{pool: pool}
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
	SELECT p.user_id, u.id IS NULL AS orphaned
	FROM page p
	LEFT JOIN LATERAL (SELECT a.id FROM auth_user_ids a WHERE a.id = p.user_id) u ON true
	WHERE EXISTS (SELECT 1 FROM auth_user_ids)
	ORDER BY p.user_id`

func (r *PostgresDeletedIdentityRepository) ListOwnersWithoutIdentity(ctx context.Context, after ids.UserId, window int) (ports.OwnerScanPage, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultOwnerScanPageTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, ownersWithoutIdentitySQL, after.UUID(), window)
	if err != nil {
		return ports.OwnerScanPage{}, database.WrapUnreadable(listOwnersWithoutIdentityOp, ports.ErrIdentityStoreUnavailable, err)
	}
	owners, err := pgx.CollectRows(rows, pgx.RowToStructByName[queueStateOwner])
	if err != nil {
		return ports.OwnerScanPage{}, database.WrapUnreadable(listOwnersWithoutIdentityOp, ports.ErrIdentityStoreUnavailable, err)
	}
	return ownerPage(owners, after, window), nil
}

type queueStateOwner struct {
	UserID   uuid.UUID
	Orphaned bool
}

func ownerPage(owners []queueStateOwner, after ids.UserId, window int) ports.OwnerScanPage {
	page := ports.OwnerScanPage{Next: after, Done: len(owners) < window}
	for _, owner := range owners {
		page.Next = ids.NewUserId(owner.UserID)
		if owner.Orphaned {
			page.Orphaned = append(page.Orphaned, page.Next)
		}
	}
	return page
}

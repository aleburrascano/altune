package persistence

import (
	"altune/go-api/internal/playback/ports"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type failingQuerier struct {
	err error
}

func (q failingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, q.err
}

func TestListOwnersWithoutIdentity_UnreadableIdentityStoreIsNotDeletedAccounts(t *testing.T) {
	unreadable := map[string]string{
		"no identity store here (a plain Postgres, no auth schema)": undefinedTableCode,
		"role denied the identity store":                            insufficientPrivCode,
	}
	for name, sqlState := range unreadable {
		t.Run(name, func(t *testing.T) {
			repo := &PgxDeletedIdentityRepository{pool: failingQuerier{err: &pgconn.PgError{Code: sqlState}}}

			owners, err := repo.ListOwnersWithoutIdentity(context.Background(), 10)

			if !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
				t.Fatalf("SQLSTATE %s = %v, want ports.ErrIdentityStoreUnavailable so the sweep idles", sqlState, err)
			}
			if owners != nil {
				t.Errorf("reported %d owners as deleted from an identity store it could not read", len(owners))
			}
		})
	}
}

func TestListOwnersWithoutIdentity_QueryFailureStaysAFailure(t *testing.T) {
	connectionRefused := errors.New("connection refused")
	repo := &PgxDeletedIdentityRepository{pool: failingQuerier{err: connectionRefused}}

	_, err := repo.ListOwnersWithoutIdentity(context.Background(), 10)

	if !errors.Is(err, connectionRefused) {
		t.Fatalf("err = %v, want the underlying failure propagated", err)
	}
	if errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Error("a failed connection was classified as an absent identity store, so the sweep would idle through an outage")
	}
}

type blockingRowsQuerier struct{}

func (blockingRowsQuerier) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestListOwnersWithoutIdentity_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	repo := &PgxDeletedIdentityRepository{pool: blockingRowsQuerier{}}
	withDeletedIdentityTimeout(50 * time.Millisecond)(repo)

	err := runWithGuard(t, func() error {
		_, err := repo.ListOwnersWithoutIdentity(context.Background(), 10)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

package persistence

import (
	"altune/go-api/internal/playback/ports"
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// failingQuerier is an identity-store read that never reaches a row, so a test
// can pin how one Postgres failure is classified.
type failingQuerier struct {
	err error
}

func (q failingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, q.err
}

// TestListOwnersWithoutIdentity_UnreadableIdentityStoreIsNotDeletedAccounts
// pins the classification the sweep's blast bound rests on: a database that
// cannot show this role the identity store must not be reported as a database
// in which every account was deleted.
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

// TestListOwnersWithoutIdentity_QueryFailureStaysAFailure keeps the idle path
// narrow: a database that is merely down must surface, or the sweep would
// report a clean run while erasing nothing, forever.
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

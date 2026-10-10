package persistence

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
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

			page, err := repo.ListOwnersWithoutIdentity(context.Background(), shared.UserId{}, 10)

			if !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
				t.Fatalf("SQLSTATE %s = %v, want ports.ErrIdentityStoreUnavailable so the sweep idles", sqlState, err)
			}
			if page.Orphaned != nil {
				t.Errorf("reported %d owners as deleted from an identity store it could not read", len(page.Orphaned))
			}
		})
	}
}

func TestListOwnersWithoutIdentity_QueryFailureStaysAFailure(t *testing.T) {
	connectionRefused := errors.New("connection refused")
	repo := &PgxDeletedIdentityRepository{pool: failingQuerier{err: connectionRefused}}

	_, err := repo.ListOwnersWithoutIdentity(context.Background(), shared.UserId{}, 10)

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
	return &blockedRows{}, ctx.Err()
}

type blockedRows struct{ pgx.Rows }

func TestListOwnersWithoutIdentity_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	repo := &PgxDeletedIdentityRepository{pool: blockingRowsQuerier{}}
	WithOwnerScanTimeout(50 * time.Millisecond)(repo)

	err := runWithGuard(t, func() error {
		_, err := repo.ListOwnersWithoutIdentity(context.Background(), shared.UserId{}, 10)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestNewPgxDeletedIdentityRepository_OwnerScanOutlastsTheRequestTimeout(t *testing.T) {
	repo := NewPgxDeletedIdentityRepository(nil)

	if got := repo.opTimeout(); got <= defaultQueueStateOpTimeout {
		t.Fatalf("owner scan timeout = %s, want a background budget longer than the %s request timeout", got, defaultQueueStateOpTimeout)
	}
}

func TestNewPgxDeletedIdentityRepository_OwnerScanTimeoutIsConfigurable(t *testing.T) {
	repo := NewPgxDeletedIdentityRepository(nil, WithOwnerScanTimeout(45*time.Second))

	if got := repo.opTimeout(); got != 45*time.Second {
		t.Fatalf("owner scan timeout = %s, want the configured 45s", got)
	}
}

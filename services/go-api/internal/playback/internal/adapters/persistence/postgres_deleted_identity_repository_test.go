package persistence_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
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
		"no identity store here (a plain Postgres, no auth schema)": "42P01",
		"role denied the identity store":                            "42501",
	}
	for name, sqlState := range unreadable {
		t.Run(name, func(t *testing.T) {
			repo := playback.NewPostgresDeletedIdentityRepository(failingQuerier{err: &pgconn.PgError{Code: sqlState}})

			page, err := repo.ListOwnersWithoutIdentity(context.Background(), ids.UserId{}, 10)

			if !errors.Is(err, playback.ErrIdentityStoreUnavailable) {
				t.Fatalf("SQLSTATE %s = %v, want playback.ErrIdentityStoreUnavailable so the sweep idles", sqlState, err)
			}
			if page.Orphaned != nil {
				t.Errorf("reported %d owners as deleted from an identity store it could not read", len(page.Orphaned))
			}
		})
	}
}

func TestListOwnersWithoutIdentity_QueryFailureStaysAFailure(t *testing.T) {
	connectionRefused := errors.New("connection refused")
	repo := playback.NewPostgresDeletedIdentityRepository(failingQuerier{err: connectionRefused})

	_, err := repo.ListOwnersWithoutIdentity(context.Background(), ids.UserId{}, 10)

	if !errors.Is(err, connectionRefused) {
		t.Fatalf("err = %v, want the underlying failure propagated", err)
	}
	if errors.Is(err, playback.ErrIdentityStoreUnavailable) {
		t.Error("a failed connection was classified as an absent identity store, so the sweep would idle through an outage")
	}
}

type timingOutRowsQuerier struct{}

func (timingOutRowsQuerier) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &blockedRows{}, timedOut(ctx, defaultOwnerScanPageTimeout)
}

type blockedRows struct{ pgx.Rows }

func TestListOwnersWithoutIdentity_DerivesDeadlineWhenPoolBlocks(t *testing.T) {
	repo := playback.NewPostgresDeletedIdentityRepository(timingOutRowsQuerier{})

	err := runWithGuard(t, func() error {
		_, err := repo.ListOwnersWithoutIdentity(context.Background(), ids.UserId{}, 10)
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

type deadlineRowsQuerier struct {
	left time.Duration
}

func (q *deadlineRowsQuerier) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if deadline, ok := ctx.Deadline(); ok {
		q.left = time.Until(deadline)
	}
	return nil, errors.New("no rows here")
}

func ownerScanDeadline(opts ...func(*playback.PostgresDeletedIdentityRepository)) time.Duration {
	q := &deadlineRowsQuerier{}
	_, _ = playback.NewPostgresDeletedIdentityRepository(q, opts...).ListOwnersWithoutIdentity(context.Background(), ids.UserId{}, 10)
	return q.left
}

func requestDeadline() time.Duration {
	q := &deadlineQuerier{}
	_ = playback.NewPostgresQueueStateRepository(q, playback.NoopQueueStateMetrics(), time.Now).DeleteForUser(context.Background(), testUser())
	return q.left
}

func TestNewPgxDeletedIdentityRepository_OwnerScanPageOutlastsTheRequestTimeout(t *testing.T) {
	got, request := ownerScanDeadline(), requestDeadline()

	if got > defaultOwnerScanPageTimeout || got < defaultOwnerScanPageTimeout-time.Second || got <= request {
		t.Fatalf("owner scan page deadline %s away, want %s, longer than the %s request deadline", got, defaultOwnerScanPageTimeout, request)
	}
}

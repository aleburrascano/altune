package housekeeping_test

import (
	"altune/go-api/internal/app/housekeeping"
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"fmt"
	"testing"
)

type erasedRows struct {
	rows int64
	err  error
}

func (e erasedRows) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	return e.rows, e.err
}

var errStoreDown = errors.New("connection refused")

func TestEraseDiscoveryRowsOfDeletedIdentities_TellsIdleApartFromFailed(t *testing.T) {
	cases := []struct {
		name    string
		erasers []discovery.DeletedIdentityEraser
		wantErr error
	}{
		{
			name: "an unreadable identity store idles",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{err: fmt.Errorf("erase: %w", discovery.ErrIdentityStoreUnavailable)},
				erasedRows{rows: 7},
			},
			wantErr: nil,
		},
		{
			name: "any other failure is reported",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{rows: 7},
				erasedRows{err: errStoreDown},
			},
			wantErr: errStoreDown,
		},
		{
			name: "a clean run reports success",
			erasers: []discovery.DeletedIdentityEraser{
				erasedRows{rows: 2},
				erasedRows{rows: 0},
			},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), tc.erasers)

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want one satisfying %v", err, tc.wantErr)
			}
		})
	}
}

type countingEraser struct {
	calls *int
	erasedRows
}

func (c countingEraser) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	*c.calls++
	return c.erasedRows.EraseRowsOfDeletedIdentities(ctx)
}

func TestDeletedIdentitySweep_AFailingTableDoesNotSkipTheRest(t *testing.T) {
	var calls int
	errDiskFull := errors.New("disk full")
	erasers := []discovery.DeletedIdentityEraser{
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errStoreDown}},
		countingEraser{calls: &calls, erasedRows: erasedRows{rows: 3}},
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errDiskFull}},
	}

	err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if calls != 3 {
		t.Errorf("erasers called = %d, want 3", calls)
	}
	if !errors.Is(err, errStoreDown) || !errors.Is(err, errDiskFull) {
		t.Errorf("error = %v, want both failures joined", err)
	}
}

func TestDeletedIdentitySweep_UnavailableStoreKeepsEarlierFailuresAndRunsTheRest(t *testing.T) {
	var calls int
	erasers := []discovery.DeletedIdentityEraser{
		countingEraser{calls: &calls, erasedRows: erasedRows{err: errStoreDown}},
		countingEraser{calls: &calls, erasedRows: erasedRows{err: discovery.ErrIdentityStoreUnavailable}},
		countingEraser{calls: &calls, erasedRows: erasedRows{rows: 4}},
	}

	err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if !errors.Is(err, errStoreDown) {
		t.Errorf("error = %v, want the earlier failure kept", err)
	}
	if calls != 3 {
		t.Errorf("erasers called = %d, want 3", calls)
	}
}

func TestDeletedIdentitySweep_EveryEraserUnavailableIsAFailure(t *testing.T) {
	erasers := []discovery.DeletedIdentityEraser{
		erasedRows{err: discovery.ErrIdentityStoreUnavailable},
		erasedRows{err: discovery.ErrIdentityStoreUnavailable},
	}

	err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), erasers)

	if !errors.Is(err, discovery.ErrIdentityStoreUnavailable) {
		t.Errorf("error = %v, want a permanently idle sweep reported as a failure", err)
	}
}

func TestEraseDiscoveryRowsOfDeletedIdentities_NoErasersIsNotAnIdleSweep(t *testing.T) {
	if err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), nil); err != nil {
		t.Errorf("error = %v, want nil: with no erasers nothing was unavailable", err)
	}
}

func TestEraseDiscoveryRowsOfDeletedIdentities_LogsTheRowsOnlyWhenSomeWereErased(t *testing.T) {
	cases := []struct {
		name    string
		erasers []discovery.DeletedIdentityEraser
		wantLog string
	}{
		{"rows erased", []discovery.DeletedIdentityEraser{erasedRows{rows: 2}, erasedRows{rows: 3}}, `level=INFO msg=discovery.deleted_identity_rows_erased rows=5` + "\n"},
		{"nothing erased", []discovery.DeletedIdentityEraser{erasedRows{}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureLogs(t, func() {
				if err := housekeeping.EraseDiscoveryRowsOfDeletedIdentities(context.Background(), tc.erasers); err != nil {
					t.Errorf("error = %v, want nil", err)
				}
			})

			if logged != tc.wantLog {
				t.Errorf("logged %q, want %q", logged, tc.wantLog)
			}
		})
	}
}

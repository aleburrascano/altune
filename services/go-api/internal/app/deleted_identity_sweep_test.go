package app

import (
	discoveryPorts "altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"testing"
)

// erasedRows is a discovery table that erases a fixed number of rows, or fails
// the way the sweep has to tell apart.
type erasedRows struct {
	rows int64
	err  error
}

func (e erasedRows) EraseRowsOfDeletedIdentities(context.Context) (int64, error) {
	return e.rows, e.err
}

var errStoreDown = errors.New("connection refused")

// TestEraseDiscoveryRowsOfDeletedIdentities_TellsIdleApartFromFailed holds the
// two answers the sweep must not confuse. An identity store this deployment
// cannot read is idle: it erases nothing and reports success, because "no
// identity is visible" must never be acted on as "every identity was deleted",
// and an hourly job that failed on every plain-Postgres deployment would be
// noise nobody reads. Any other failure is reported, so the job's health signal
// degrades and the erasure is retried rather than counted as done.
func TestEraseDiscoveryRowsOfDeletedIdentities_TellsIdleApartFromFailed(t *testing.T) {
	cases := []struct {
		name    string
		erasers []discoveryPorts.DeletedIdentityEraser
		wantErr error
	}{
		{
			name: "an unreadable identity store idles",
			erasers: []discoveryPorts.DeletedIdentityEraser{
				erasedRows{err: fmt.Errorf("erase: %w", discoveryPorts.ErrIdentityStoreUnavailable)},
				erasedRows{rows: 7},
			},
			wantErr: nil,
		},
		{
			name: "any other failure is reported",
			erasers: []discoveryPorts.DeletedIdentityEraser{
				erasedRows{rows: 7},
				erasedRows{err: errStoreDown},
			},
			wantErr: errStoreDown,
		},
		{
			name: "a clean run reports success",
			erasers: []discoveryPorts.DeletedIdentityEraser{
				erasedRows{rows: 2},
				erasedRows{rows: 0},
			},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := eraseDiscoveryRowsOfDeletedIdentities(context.Background(), tc.erasers)

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want one satisfying %v", err, tc.wantErr)
			}
		})
	}
}

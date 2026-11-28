package monitoringfromsearch_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromsearch"
	"altune/go-api/internal/search"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCoverageStore struct {
	total    int
	totalErr error
	rows     []search.QueryCount
	rowsErr  error
	gotLimit int
}

func (f *fakeCoverageStore) ZeroResultTotal(context.Context, time.Time) (int, error) {
	return f.total, f.totalErr
}

func (f *fakeCoverageStore) NoResultSearches(_ context.Context, _ time.Time, limit int) ([]search.QueryCount, error) {
	f.gotLimit = limit
	return f.rows, f.rowsErr
}

func TestCoverageEvents_PassesTheTotalAndItsFailureThrough(t *testing.T) {
	boom := errors.New("boom")

	got, err := monitoringfromsearch.NewCoverageEvents(&fakeCoverageStore{total: 31}).ZeroResultTotal(context.Background(), time.Time{})
	if err != nil || got != 31 {
		t.Fatalf("ZeroResultTotal = (%d, %v), want (31, nil)", got, err)
	}
	if _, err := monitoringfromsearch.NewCoverageEvents(&fakeCoverageStore{totalErr: boom}).ZeroResultTotal(context.Background(), time.Time{}); !errors.Is(err, boom) {
		t.Fatalf("ZeroResultTotal error = %v, want the store's failure", err)
	}
}

func TestCoverageEvents_ReportsTheMostSearchedMissingQueryAndNothingOnFailureOrNoRows(t *testing.T) {
	cases := []struct {
		name      string
		store     *fakeCoverageStore
		wantHits  int
		wantFound bool
	}{
		{"the top row", &fakeCoverageStore{rows: []search.QueryCount{{QueryNorm: "a", Count: 20}, {QueryNorm: "b", Count: 3}}}, 20, true},
		{"no rows", &fakeCoverageStore{}, 0, false},
		{"the read fails", &fakeCoverageStore{rows: []search.QueryCount{{Count: 9}}, rowsErr: errors.New("down")}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits, found := monitoringfromsearch.NewCoverageEvents(tc.store).TopNoResultQueryHits(context.Background(), time.Time{})

			if hits != tc.wantHits || found != tc.wantFound {
				t.Errorf("TopNoResultQueryHits = (%d, %v), want (%d, %v)", hits, found, tc.wantHits, tc.wantFound)
			}
			if tc.store.gotLimit != 1000 {
				t.Errorf("asked the store for %d rows, want 1000", tc.store.gotLimit)
			}
		})
	}
}

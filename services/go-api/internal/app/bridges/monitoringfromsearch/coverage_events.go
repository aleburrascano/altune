package monitoringfromsearch

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
	"fmt"
	"time"
)

const topQueryScan = 1000

type CoverageStore interface {
	ZeroResultTotal(ctx context.Context, since time.Time) (int, error)
	NoResultSearches(ctx context.Context, since time.Time, limit int) ([]search.QueryCount, error)
}

type coverageEvents struct {
	store CoverageStore
}

func NewCoverageEvents(store CoverageStore) monitoring.CoverageEvents {
	return coverageEvents{store: store}
}

func (c coverageEvents) ZeroResultTotal(ctx context.Context, since time.Time) (int, error) {
	total, err := c.store.ZeroResultTotal(ctx, since)
	if err != nil {
		return 0, fmt.Errorf("zero result total: %w", err)
	}
	return total, nil
}

func (c coverageEvents) TopNoResultQueryHits(ctx context.Context, since time.Time) (int, bool) {
	rows, err := c.store.NoResultSearches(ctx, since, topQueryScan)
	if err != nil || len(rows) == 0 {
		return 0, false
	}
	return rows[0].Count, true
}

package monitoringfromsearch

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"context"
	"fmt"
	"time"
)

type discographyQualityReader struct {
	store search.DiscographyQualityReader
}

func NewDiscographyQualityReader(store search.DiscographyQualityReader) monitoring.DiscographyQualityReader {
	return discographyQualityReader{store: store}
}

func (r discographyQualityReader) DiscographyQuality(ctx context.Context, since time.Time, groupBy monitoring.DiscographyGroupBy, limit int) ([]monitoring.DiscographyCase, error) {
	grouping, err := search.ParseDiscographyGroupBy(string(groupBy))
	if err != nil {
		return nil, fmt.Errorf("discography quality: %w", err)
	}
	cases, err := r.store.DiscographyQuality(ctx, since, grouping, limit)
	if err != nil {
		return nil, err
	}
	out := make([]monitoring.DiscographyCase, len(cases))
	for i, c := range cases {
		out[i] = monitoring.DiscographyCase(c)
	}
	return out, nil
}

func (r discographyQualityReader) SuspectRate(ctx context.Context, since time.Time) (monitoring.DiscographySuspectRate, error) {
	rate, err := r.store.SuspectRate(ctx, since)
	if err != nil {
		return monitoring.DiscographySuspectRate{}, err
	}
	return monitoring.DiscographySuspectRate(rate), nil
}

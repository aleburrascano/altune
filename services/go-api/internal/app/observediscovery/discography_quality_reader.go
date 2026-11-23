package observediscovery

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
	"fmt"
	"time"
)

type discographyQualityReader struct {
	store discovery.DiscographyQualityReader
}

func NewDiscographyQualityReader(store discovery.DiscographyQualityReader) observe.DiscographyQualityReader {
	return discographyQualityReader{store: store}
}

func (r discographyQualityReader) DiscographyQuality(ctx context.Context, since time.Time, groupBy observe.DiscographyGroupBy, limit int) ([]observe.DiscographyCase, error) {
	grouping := discovery.ParseDiscographyGroupBy(string(groupBy))
	if string(grouping) != string(groupBy) {
		return nil, fmt.Errorf("discography quality: discovery has no grouping for %q", groupBy)
	}
	cases, err := r.store.DiscographyQuality(ctx, since, grouping, limit)
	if err != nil {
		return nil, err
	}
	out := make([]observe.DiscographyCase, len(cases))
	for i, c := range cases {
		out[i] = observe.DiscographyCase(c)
	}
	return out, nil
}

func (r discographyQualityReader) SuspectRate(ctx context.Context, since time.Time) (observe.DiscographySuspectRate, error) {
	rate, err := r.store.SuspectRate(ctx, since)
	if err != nil {
		return observe.DiscographySuspectRate{}, err
	}
	return observe.DiscographySuspectRate(rate), nil
}

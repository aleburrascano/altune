package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"context"
	"errors"
	"fmt"
	"time"
)

func queryDiscographyQuality(ctx context.Context, reader ports.DiscographyQualityReader, since time.Time, groupBy ports.DiscographyGroupBy, limit int) (discographyQualityResult, error) {
	cases, err := reader.DiscographyQuality(ctx, since, groupBy, limit)
	if err != nil {
		return discographyQualityResult{}, mapQueryError(err)
	}
	suspect, err := reader.SuspectRate(ctx, since)
	if err != nil {
		return discographyQualityResult{}, mapQueryError(err)
	}
	return discographyQualityResult{cases: toDiscographyCaseDTOs(cases), suspectRate: suspect.Rate, lastSample: suspect.LastSample.UTC()}, nil
}

func mapQueryError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", errDiscographyQualityTimeout, err)
	}
	return err
}

func toDiscographyCaseDTOs(cases []ports.DiscographyCase) []discographyCaseDTO {
	out := make([]discographyCaseDTO, 0, len(cases))
	for _, c := range cases {
		counts := c.ProviderCounts
		if counts == nil {
			counts = map[string]int{}
		}
		out = append(out, discographyCaseDTO{
			Artist:             c.ArtistRef,
			ArtistRef:          c.ArtistRef,
			Releases:           c.Releases,
			SingleProvider:     c.SingleProvider,
			SingleProviderNoID: c.SingleProviderNoID,
			ProviderCounts:     counts,
			LastSeen:           c.LastSeen.UTC(),
		})
	}
	return out
}

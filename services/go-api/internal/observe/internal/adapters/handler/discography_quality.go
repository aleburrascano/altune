package handler

import (
	"altune/go-api/internal/observe/internal/core/ports"
	"context"
	"errors"
	"fmt"
	"time"
)

type discographyCaseDTO struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

type discographyQualityResult struct {
	cases       []discographyCaseDTO
	suspectRate float64
	lastSample  time.Time
}

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

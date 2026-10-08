package discoverybridge

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/observe/domain"
	"context"
	"errors"
	"time"
)

type DiscographyQualityReader = ports.DiscographyQualityReader

type DiscographyGroupBy = ports.DiscographyGroupBy

type DiscographyCaseDTO struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

func ParseDiscographyGroupBy(raw string) DiscographyGroupBy {
	return ports.ParseDiscographyGroupBy(raw)
}

func QueryDiscographyQuality(ctx context.Context, reader DiscographyQualityReader, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCaseDTO, float64, time.Time, error) {
	cases, err := reader.DiscographyQuality(ctx, since, groupBy, limit)
	if err != nil {
		return nil, 0, time.Time{}, mapQueryError(err)
	}
	suspect, err := reader.SuspectRate(ctx, since)
	if err != nil {
		return nil, 0, time.Time{}, mapQueryError(err)
	}
	return toDiscographyCaseDTOs(cases), suspect.Rate, suspect.LastSample.UTC(), nil
}

func mapQueryError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ErrDiscographyQualityTimeout
	}
	return err
}

func toDiscographyCaseDTOs(cases []ports.DiscographyCase) []DiscographyCaseDTO {
	out := make([]DiscographyCaseDTO, 0, len(cases))
	for _, c := range cases {
		counts := c.ProviderCounts
		if counts == nil {
			counts = map[string]int{}
		}
		out = append(out, DiscographyCaseDTO{
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

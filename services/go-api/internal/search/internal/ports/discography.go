package ports

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type DiscographyCase struct {
	ArtistRef          string
	Releases           int
	SingleProvider     int
	SingleProviderNoID int
	ProviderCounts     map[string]int
	LastSeen           time.Time
}

type DiscographyGroupBy string

const (
	GroupByArtist            DiscographyGroupBy = "artist"
	GroupByProvider          DiscographyGroupBy = "provider"
	GroupByContaminationBand DiscographyGroupBy = "contamination_band"
)

var ErrUnknownDiscographyGroupBy = errors.New("unknown discography grouping")

func ParseDiscographyGroupBy(raw string) (DiscographyGroupBy, error) {
	switch groupBy := DiscographyGroupBy(raw); groupBy {
	case GroupByArtist, GroupByProvider, GroupByContaminationBand:
		return groupBy, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownDiscographyGroupBy, raw)
	}
}

type DiscographySuspectRate struct {
	Rate       float64
	LastSample time.Time
}

type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
	SuspectRate(ctx context.Context, since time.Time) (DiscographySuspectRate, error)
}

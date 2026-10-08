package ports

import (
	"context"
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

func ParseDiscographyGroupBy(raw string) DiscographyGroupBy {
	switch DiscographyGroupBy(raw) {
	case GroupByProvider:
		return GroupByProvider
	case GroupByContaminationBand:
		return GroupByContaminationBand
	default:
		return GroupByArtist
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

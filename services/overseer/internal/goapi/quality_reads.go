package goapi

import (
	"context"
	"time"
)

const observeDiscographyQualityPath = "/observe/quality/discography"

type DiscographyQuality struct {
	WindowDays   int               `json:"window_days"`
	GroupBy      string            `json:"group_by"`
	Cases        []DiscographyCase `json:"cases"`
	SuspectRate  float64           `json:"suspect_rate"`
	LastSampleAt time.Time         `json:"last_sample_at"`
}

type DiscographyCase struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

func (c *Client) AdminDiscographyQuality(ctx context.Context) (DiscographyQuality, error) {
	var out DiscographyQuality
	if err := c.get(ctx, observeDiscographyQualityPath, &out); err != nil {
		return DiscographyQuality{}, err
	}
	return out, nil
}

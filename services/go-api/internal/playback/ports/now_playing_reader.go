package ports

import (
	"altune/go-api/internal/shared"
	"context"
)

type NowPlayingTrack struct {
	Id                string
	Title             string
	Artist            string
	ArtworkURL        *string
	DurationSeconds   *float64
	AcquisitionStatus string
}

type NowPlayingReader interface {
	Lookup(ctx context.Context, userId shared.UserId, trackId string) (*NowPlayingTrack, error)
}

package ports

import (
	"altune/go-api/internal/shared"
	"context"
)

type OwnedTrack struct {
	TrackID           string
	Title             string
	Artist            string
	AcquisitionStatus string
	TrackNumber       *int
}

type TrackNumberFiller interface {
	FillTrackNumber(ctx context.Context, userId shared.UserId, trackId string, trackNumber int) error
}

type OwnershipReader interface {
	OwnedByTitleArtist(ctx context.Context, userId shared.UserId) (map[string]OwnedTrack, error)
}

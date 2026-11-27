package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
)

type OwnedTrack struct {
	TrackID           string
	Title             string
	Artist            string
	AcquisitionStatus string
	TrackNumber       *int
	ISRC              string
	Album             string
}

type TrackNumberFiller interface {
	FillTrackNumber(ctx context.Context, userId ids.UserId, trackId ids.TrackId, trackNumber int) error
}

type OwnershipReader interface {
	OwnedByTitleArtist(ctx context.Context, userId ids.UserId) (map[string]OwnedTrack, error)
}

type OwnedTrackLister interface {
	ListOwnedTracks(ctx context.Context, userId ids.UserId) ([]OwnedTrack, error)
}

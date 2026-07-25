package ports

import (
	"context"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
)

type OwnedTrack struct {
	TrackID           string
	AcquisitionStatus string
	TrackNumber       *int
}

type TrackNumberFiller interface {
	FillTrackNumber(ctx context.Context, userId shared.UserId, trackId string, trackNumber int) error
}

func OwnershipKey(title, artist string) string {
	return textnorm.NormalizeForMatch(title) + "|" + textnorm.NormalizeForMatch(artist)
}

type OwnershipReader interface {
	OwnedByTitleArtist(ctx context.Context, userId shared.UserId) (map[string]OwnedTrack, error)
}

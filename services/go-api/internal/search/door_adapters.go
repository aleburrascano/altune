package search

import (
	domain "altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
)

type (
	FeaturedArtist    = domain.FeaturedArtist
	OwnedTrack        = ports.OwnedTrack
	OwnedTrackLister  = ports.OwnedTrackLister
	TrackNumberFiller = ports.TrackNumberFiller
)

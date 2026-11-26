package search

import (
	ports "altune/go-api/internal/search/internal/core/ports"
	domain "altune/go-api/internal/search/internal/domain"
)

type (
	FeaturedArtist    = domain.FeaturedArtist
	OwnedTrack        = ports.OwnedTrack
	OwnedTrackLister  = ports.OwnedTrackLister
	TrackNumberFiller = ports.TrackNumberFiller
)

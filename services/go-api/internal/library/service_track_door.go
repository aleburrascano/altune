package library

import track "altune/go-api/internal/library/service/track"

type (
	AddTrackInput         = track.AddTrackInput
	AddTrackService       = track.AddTrackService
	DeleteTrackService    = track.DeleteTrackService
	FeaturedArtistDTO     = track.FeaturedArtistDTO
	GetTrackStatusService = track.GetTrackStatusService
	ListTracksService     = track.ListTracksService
	SetTrackNumberService = track.SetTrackNumberService
)

const MaxFeaturedArtistsPerTrack = track.MaxFeaturedArtistsPerTrack

var (
	NewAddTrackService       = track.NewAddTrackService
	NewDeleteTrackService    = track.NewDeleteTrackService
	NewGetTrackStatusService = track.NewGetTrackStatusService
	NewListTracksService     = track.NewListTracksService
	NewSetTrackNumberService = track.NewSetTrackNumberService
)

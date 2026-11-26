package library

import track "altune/go-api/internal/library/internal/service/track"

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
	ErrTrackClockRequired        = track.ErrClockRequired
	ErrTrackCollaboratorRequired = track.ErrCollaboratorRequired
	MustNewAddTrackService       = track.MustNewAddTrackService
	NewAddTrackService           = track.NewAddTrackService
	NewDeleteTrackService        = track.NewDeleteTrackService
	NewGetTrackStatusService     = track.NewGetTrackStatusService
	NewListTracksService         = track.NewListTracksService
	NewSetTrackNumberService     = track.NewSetTrackNumberService
)

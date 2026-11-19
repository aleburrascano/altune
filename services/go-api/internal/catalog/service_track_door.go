package catalog

import track "altune/go-api/internal/catalog/service/track"

type (
	AddTrackInput         = track.AddTrackInput
	AddTrackService       = track.AddTrackService
	FeaturedArtistDTO     = track.FeaturedArtistDTO
	DeleteTrackService    = track.DeleteTrackService
	GetTrackStatusService = track.GetTrackStatusService
	ListTracksService     = track.ListTracksService
	SetTrackNumberService = track.SetTrackNumberService
)

var ErrLibraryFull = track.ErrLibraryFull

const MaxFeaturedArtistsPerTrack = track.MaxFeaturedArtistsPerTrack

var (
	NewAddTrackService         = track.NewAddTrackService
	NewDeleteTrackService      = track.NewDeleteTrackService
	NewGetTrackStatusService   = track.NewGetTrackStatusService
	NewListTracksService       = track.NewListTracksService
	NewSetTrackNumberService   = track.NewSetTrackNumberService
	WithAcquisitionScheduler   = track.WithAcquisitionScheduler
	WithAddTrackClock          = track.WithAddTrackClock
	WithAddTrackEvents         = track.WithAddTrackEvents
	WithDeleteTrackEvents      = track.WithDeleteTrackEvents
	WithDeleteTrackMetrics     = track.WithDeleteTrackMetrics
	WithDeleteTrackOrphanQueue = track.WithDeleteTrackOrphanQueue
)

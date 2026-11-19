package catalog

import domain "altune/go-api/internal/catalog/domain"

type (
	SearchKeys = domain.SearchKeys
	Track      = domain.Track
	TrackId    = domain.TrackId
)

type (
	Playlist        = domain.Playlist
	PlaylistId      = domain.PlaylistId
	PlaylistSummary = domain.PlaylistSummary
	PlaylistTrack   = domain.PlaylistTrack
)

var (
	ErrPlaylistFull           = domain.ErrPlaylistFull
	ErrTrackAlreadyInPlaylist = domain.ErrTrackAlreadyInPlaylist
)

var (
	CleanSearchKeys           = domain.CleanSearchKeys
	KnownCatalogProviders     = domain.KnownCatalogProviders
	NewTrack                  = domain.NewTrack
	NewTrackId                = domain.NewTrackId
	NormalizeCatalogMBID      = domain.NormalizeCatalogMBID
	ParseTrackId              = domain.ParseTrackId
	TrackDedupKey             = domain.TrackDedupKey
	ValidateOptionalTrackText = domain.ValidateOptionalTrackText
)

var (
	ErrAudioNotAvailable           = domain.ErrAudioNotAvailable
	ErrAudioTemporarilyUnavailable = domain.ErrAudioTemporarilyUnavailable
)

type AcquisitionStatus = domain.AcquisitionStatus

const (
	AcquisitionFailed  = domain.AcquisitionFailed
	AcquisitionPending = domain.AcquisitionPending
	AcquisitionReady   = domain.AcquisitionReady
)

type (
	AlbumGroup     = domain.AlbumGroup
	CatalogSource  = domain.CatalogSource
	FeaturedArtist = domain.FeaturedArtist
	LibraryQuery   = domain.LibraryQuery
)

const (
	MaxCatalogSources      = domain.MaxCatalogSources
	MaxLibraryPageSize     = domain.MaxLibraryPageSize
	MaxLibrarySearchLength = domain.MaxLibrarySearchLength
	SortYear               = domain.SortYear
)

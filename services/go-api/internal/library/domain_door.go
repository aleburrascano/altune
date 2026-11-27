package library

import domain "altune/go-api/internal/library/internal/domain"

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
	ErrIllegalAcquisitionTransition = domain.ErrIllegalAcquisitionTransition
	ErrIDSourceRequired             = domain.ErrIDSourceRequired
	ErrTrackTimestampClockRequired  = domain.ErrTrackTimestampClockRequired
	ErrLibraryFull                  = domain.ErrLibraryFull
	ErrPlaylistFull                 = domain.ErrPlaylistFull
	ErrStorageStalled               = domain.ErrStorageStalled
	ErrTrackAlreadyInPlaylist       = domain.ErrTrackAlreadyInPlaylist
)

var (
	CleanSearchKeys           = domain.CleanSearchKeys
	FailureMessage            = domain.FailureMessage
	JoinFailureReason         = domain.JoinFailureReason
	KnownCatalogProviders     = domain.KnownCatalogProviders
	NewFeaturedArtist         = domain.NewFeaturedArtist
	NewPlaylist               = domain.NewPlaylist
	NewTrack                  = domain.NewTrack
	NewValidationError        = domain.NewValidationError
	NormalizeCatalogMBID      = domain.NormalizeCatalogMBID
	NormalizeCatalogSources   = domain.NormalizeCatalogSources
	NormalizePage             = domain.NormalizePage
	ParseAcquisitionStatus    = domain.ParseAcquisitionStatus
	ParseLibrarySort          = domain.ParseLibrarySort
	ParsePlaylistId           = domain.ParsePlaylistId
	ParseTrackId              = domain.ParseTrackId
	PlaylistIdFromUUID        = domain.PlaylistIdFromUUID
	SplitFailureReason        = domain.SplitFailureReason
	TotalDurationSeconds      = domain.TotalDurationSeconds
	TrackDedupKey             = domain.TrackDedupKey
	ValidateFeaturedArtist    = domain.ValidateFeaturedArtist
	ValidateOptionalTrackText = domain.ValidateOptionalTrackText
	ValidateSourceURL         = domain.ValidateSourceURL
	AttachSoundCloudURL       = domain.AttachSoundCloudURL
)

var (
	ErrAudioNotAvailable           = domain.ErrAudioNotAvailable
	ErrAudioTemporarilyUnavailable = domain.ErrAudioTemporarilyUnavailable
)

type DownloadStatus = domain.DownloadStatus

const (
	AcquisitionFailed  = domain.AcquisitionFailed
	AcquisitionPending = domain.AcquisitionPending
	AcquisitionReady   = domain.AcquisitionReady
)

type (
	AlbumGroup     = domain.AlbumGroup
	CatalogSource  = domain.CatalogSource
	FailureCode    = domain.FailureCode
	FeaturedArtist = domain.FeaturedArtist
	LibraryQuery   = domain.LibraryQuery
	LibrarySort    = domain.LibrarySort
)

const (
	FailureAcquisitionInterrupted = domain.FailureAcquisitionInterrupted
	FailureAcquisitionRefused     = domain.FailureAcquisitionRefused
	FailureNoConfidentMatch       = domain.FailureNoConfidentMatch
	MaxCatalogSources             = domain.MaxCatalogSources
	MaxLibraryPageSize            = domain.MaxLibraryPageSize
	MaxLibrarySearchLength        = domain.MaxLibrarySearchLength
	MaxPlaylistTracks             = domain.MaxPlaylistTracks
	ProvenanceVerified            = domain.ProvenanceVerified
	RoleFeatured                  = domain.RoleFeatured
	SortAlphabetical              = domain.SortAlphabetical
	SortRecent                    = domain.SortRecent
	SortYear                      = domain.SortYear
)

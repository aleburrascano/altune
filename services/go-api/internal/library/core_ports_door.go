package library

import ports "altune/go-api/internal/library/internal/core/ports"

type (
	InPlaceVerdict        = ports.InPlaceVerdict
	AcquisitionScheduler  = ports.AcquisitionScheduler
	AudioAgeLister        = ports.AudioAgeLister
	AudioLister           = ports.AudioLister
	AudioRefPrefixLister  = ports.AudioRefPrefixLister
	AudioStore            = ports.AudioStore
	AudioStream           = ports.AudioStream
	AudioLinkSigner       = ports.AudioLinkSigner
	ObjectAge             = ports.ObjectAge
	OrphanedAudio         = ports.OrphanedAudio
	OrphanedAudioQueue    = ports.OrphanedAudioQueue
	OrphanedAudioRecorder = ports.OrphanedAudioRecorder
	ReadyAudioTrack       = ports.ReadyAudioTrack
	TrackReadWriter       = ports.TrackReadWriter
)

var (
	NoopAcquisitionScheduler = ports.NoopAcquisitionScheduler
	NoopAudioStoreMetrics    = ports.NoopAudioStoreMetrics
)

var (
	ErrDBTransient                  = ports.ErrDBTransient
	ErrPlaylistChangedDuringReorder = ports.ErrPlaylistChangedDuringReorder
	ErrPlaylistNotOwned             = ports.ErrPlaylistNotOwned
	ErrTrackMissing                 = ports.ErrTrackMissing
	ErrTrackVersionConflict         = ports.ErrTrackVersionConflict
	ErrTrackFenceSuperseded         = ports.ErrTrackFenceSuperseded
)

type (
	FeaturedArtistCredit       = ports.FeaturedArtistCredit
	FeaturedArtistCreditSource = ports.FeaturedArtistCreditSource
	FeaturedArtistResolver     = ports.FeaturedArtistResolver
)

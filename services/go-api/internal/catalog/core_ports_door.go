package catalog

import ports "altune/go-api/internal/catalog/internal/core/ports"

type (
	AcquisitionScheduler  = ports.AcquisitionScheduler
	AudioAgeLister        = ports.AudioAgeLister
	AudioLister           = ports.AudioLister
	AudioRefPrefixLister  = ports.AudioRefPrefixLister
	AudioStore            = ports.AudioStore
	AudioStream           = ports.AudioStream
	AudioURLSigner        = ports.AudioURLSigner
	ObjectAge             = ports.ObjectAge
	OrphanedAudio         = ports.OrphanedAudio
	OrphanedAudioQueue    = ports.OrphanedAudioQueue
	OrphanedAudioRecorder = ports.OrphanedAudioRecorder
	ReadyAudioTrack       = ports.ReadyAudioTrack
	TrackReadWriter       = ports.TrackReadWriter
)

var (
	ErrDBTransient                  = ports.ErrDBTransient
	ErrPlaylistChangedDuringReorder = ports.ErrPlaylistChangedDuringReorder
	ErrTrackMissing                 = ports.ErrTrackMissing
	ErrTrackVersionConflict         = ports.ErrTrackVersionConflict
)

type FeaturedArtistResolver = ports.FeaturedArtistResolver

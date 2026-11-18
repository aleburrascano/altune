package catalog

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

type (
	InMemoryAudioStore         = memory.AudioStore
	InMemoryAudioStoreMetrics  = memory.Metrics
	InMemoryFeaturedResolver   = memory.FeaturedResolver
	InMemoryOrphanedAudioQueue = memory.OrphanedAudioQueue
	InMemoryPlaylistRepo       = memory.PlaylistRepo
	InMemoryTrackRepo          = memory.TrackRepo
)

var (
	AssertAttrs                   = memory.AssertAttrs
	CaptureAuditLogs              = memory.CaptureAuditLogs
	FixtureOtherUserId            = memory.OtherUserId
	FixtureUserId                 = memory.UserId
	NewInMemoryAudioStore         = memory.NewAudioStore
	NewInMemoryOrphanedAudioQueue = memory.NewOrphanedAudioQueue
	NewInMemoryPlaylistRepo       = memory.NewPlaylistRepo
	NewInMemoryTrackRepo          = memory.NewTrackRepo
	SeedPlaylist                  = memory.SeedPlaylist
	SeedReadyTrack                = memory.SeedReadyTrack
	SeedTrack                     = memory.SeedTrack
)

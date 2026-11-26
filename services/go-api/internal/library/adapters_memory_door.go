package library

import (
	memory "altune/go-api/internal/library/internal/adapters/memory"
)

type (
	InMemoryAudioStore         = memory.AudioStore
	InMemoryAudioStoreMetrics  = memory.Metrics
	InMemoryFeaturedResolver   = memory.FeaturedResolver
	InMemoryOrphanedAudioQueue = memory.OrphanedAudioQueue
	InMemoryPlaylistRepo       = memory.PlaylistRepo
	InMemoryRecordingPublisher = memory.RecordingPlaylistPublisher
	InMemoryScheduler          = memory.Scheduler
	InMemoryStuckScheduler     = memory.StuckScheduler
	InMemoryTrackRepo          = memory.TrackRepo
)

var (
	AssertAttrs                   = memory.AssertAttrs
	AssertScheduleDeadline        = memory.AssertScheduleDeadline
	CaptureAuditLogs              = memory.CaptureAuditLogs
	NewInMemoryAudioStore         = memory.NewAudioStore
	NewInMemoryOrphanedAudioQueue = memory.NewOrphanedAudioQueue
	NewInMemoryPlaylistRepo       = memory.NewPlaylistRepo
	NewInMemoryTrackRepo          = memory.NewTrackRepo
	WithInMemoryLibraryCap        = memory.WithLibraryCap
)

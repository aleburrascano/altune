package download

import ports "altune/go-api/internal/download/internal/core/ports"

type (
	Status               = ports.Status
	Verification         = ports.Verification
	AudioIdentifier      = ports.AudioIdentifier
	AudioSource          = ports.AudioSource
	CatalogSource        = ports.CatalogSource
	FetchedAudio         = ports.FetchedAudio
	JobRecord            = ports.JobRecord
	PreviewMatcher       = ports.PreviewMatcher
	RecordingMatch       = ports.RecordingMatch
	RecordingQuery       = ports.RecordingQuery
	TrackLedgerPort      = ports.LibraryTracks
	TrackSnapshot        = ports.TrackSnapshot
	TrackStatus          = ports.TrackStatus
	UpgradeState         = ports.UpgradeState
	UpgradeStateSnapshot = ports.UpgradeStateSnapshot
	InPlaceRecord        = ports.InPlaceRecord
	InPlaceStore         = ports.InPlaceStore
)

const ProviderYouTube = ports.ProviderYouTube

var MergeUpgradeEvidence = ports.MergeUpgradeEvidence

type (
	AudioCandidate = ports.AudioCandidate
	Completion     = ports.Completion
	FindRequest    = ports.FindRequest
	OrphanedAudio  = ports.OrphanedAudio
	ProviderRef    = ports.ProviderRef
)

const (
	TrackPending = ports.TrackPending
	TrackReady   = ports.TrackReady
)

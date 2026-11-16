package acquisition

import ports "altune/go-api/internal/acquisition/internal/core/ports"

type (
	AcoustIDResult          = ports.AcoustIDResult
	AcquisitionStatus       = ports.AcquisitionStatus
	AcquisitionVerification = ports.AcquisitionVerification
	AudioIdentifier         = ports.AudioIdentifier
	AudioSource             = ports.AudioSource
	AudioTagger             = ports.AudioTagger
	CatalogSource           = ports.CatalogSource
	FetchedAudio            = ports.FetchedAudio
	JobKind                 = ports.JobKind
	JobRecord               = ports.JobRecord
	PreviewComparison       = ports.PreviewComparison
	PreviewMatcher          = ports.PreviewMatcher
	PreviewTarget           = ports.PreviewTarget
	RecordingIdentity       = ports.RecordingIdentity
	RecordingMatch          = ports.RecordingMatch
	RecordingQuery          = ports.RecordingQuery
	TrackLedgerPort         = ports.TrackLedger
	TrackSnapshot           = ports.TrackSnapshot
	TrackTags               = ports.TrackTags
)

const (
	JobKindUpgrade  = ports.JobKindUpgrade
	ProviderYouTube = ports.ProviderYouTube
)

type (
	AudioCandidate = ports.AudioCandidate
	CandidateKind  = ports.CandidateKind
	Completion     = ports.Completion
	FindRequest    = ports.FindRequest
	OrphanedAudio  = ports.OrphanedAudio
	ProviderRef    = ports.ProviderRef
	SourceFormat   = ports.SourceFormat
)

const (
	KindArtTrack    = ports.KindArtTrack
	KindPinnedVideo = ports.KindPinnedVideo
	KindUpload      = ports.KindUpload
)

const (
	TrackPending = ports.TrackPending
	TrackReady   = ports.TrackReady
)

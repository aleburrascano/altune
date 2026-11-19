package acquisition

import service "altune/go-api/internal/acquisition/service"

type (
	AcquireTrackAudioService       = service.AcquireTrackAudioService
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	ConfidenceEvidence             = service.Evidence
	TrackRef                       = service.TrackRef
	VerdictKind                    = service.VerdictKind
	VerifySkipCounter              = service.VerifySkipCounter
)

var (
	AudioVerdictOf                    = service.AudioVerdictOf
	BuildLegacyAudioRef               = service.BuildLegacyAudioRef
	BuildLegacyAudioRefUncapped       = service.BuildLegacyAudioRefUncapped
	FingerprintOf                     = service.FingerprintOf
	NewAcquireTrackAudioService       = service.NewAcquireTrackAudioService
	NewBackgroundAcquisitionScheduler = service.NewBackgroundAcquisitionScheduler
	NewDownloadLimiter                = service.NewDownloadLimiter
	NewReacquireAdmission             = service.NewReacquireAdmission
	NewRetryAdmission                 = service.NewRetryAdmission
	NewSourceRegistry                 = service.NewSourceRegistry
	ScoreConfidence                   = service.ScoreConfidence
	SourceKindOf                      = service.SourceKindOf
	SweepStaleTempDirs                = service.SweepStaleTempDirs
	UnrequestedQualifiers             = service.UnrequestedQualifiers
	UploaderSourceKind                = service.UploaderSourceKind
	WithAcquireConfidenceFloor        = service.WithAcquireConfidenceFloor
	WithAcquireEvents                 = service.WithAcquireEvents
	WithAcquireOrphanQueue            = service.WithAcquireOrphanQueue
	WithAcquireRecordingLookup        = service.WithAcquireRecordingLookup
	WithAcquireStoreKeyPrefix         = service.WithAcquireStoreKeyPrefix
	WithAcquireTempWorkspace          = service.WithAcquireTempWorkspace
	WithAudioIdentifier               = service.WithAudioIdentifier
	WithAudioProber                   = service.WithAudioProber
	WithAudioTagger                   = service.WithAudioTagger
	WithDownloadLimiter               = service.WithDownloadLimiter
	WithDrainBudget                   = service.WithDrainBudget
	WithFingerprintVerified           = service.WithFingerprintVerified
	WithJobNotifier                   = service.WithJobNotifier
	WithJobQueue                      = service.WithJobQueue
	WithOutcomeRecorder               = service.WithOutcomeRecorder
	WithPreviewMatcher                = service.WithPreviewMatcher
	WithRecordingResolver             = service.WithRecordingResolver
	WithRejectionStore                = service.WithRejectionStore
	WithRerecordingFinder             = service.WithRerecordingFinder
	WithSchedulerEvents               = service.WithSchedulerEvents
	WithSchedulerPaused               = service.WithSchedulerPaused
	WithStoredAudioReader             = service.WithStoredAudioReader
	WithSupersededAudioGrace          = service.WithSupersededAudioGrace
	WithUpgradeLane                   = service.WithUpgradeLane
	WithVerificationStatus            = service.WithVerificationStatus
	WithVerifySkipCount               = service.WithVerifySkipCount
	WithVerifySkips                   = service.WithVerifySkips
)

type DownloadLimiter = service.DownloadLimiter

const (
	VerdictContradicted  = service.VerdictContradicted
	VerdictDifferentSong = service.VerdictDifferentSong
	VerdictHard          = service.VerdictHard
	VerdictOtherVersion  = service.VerdictOtherVersion
	VerdictSoft          = service.VerdictSoft
	VerdictUnknown       = service.VerdictUnknown
)

var ErrInPlaceUnavailable = service.ErrInPlaceUnavailable

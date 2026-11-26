package download

import service "altune/go-api/internal/download/service"

type (
	AcquireTrackAudioService       = service.AcquireTrackAudioService
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	ConfidenceEvidence             = service.Evidence
	TrackRef                       = service.TrackRef
	VerdictKind                    = service.VerdictKind
	StagePorts                     = service.StagePorts
	VerificationPorts              = service.VerificationPorts
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
	DirectDeleteOrphans               = service.DirectDeleteOrphans
	SweepStaleTempDirs                = service.SweepStaleTempDirs
	UploaderSourceKind                = service.UploaderSourceKind
	WithAcquireConfidenceFloor        = service.WithAcquireConfidenceFloor
	WithAcquireEvents                 = service.WithAcquireEvents
	WithAcquireStoreKeyPrefix         = service.WithAcquireStoreKeyPrefix
	WithDownloadLimiter               = service.WithDownloadLimiter
	WithDrainBudget                   = service.WithDrainBudget
	WithFingerprintVerified           = service.WithFingerprintVerified
	WithJobNotifier                   = service.WithJobNotifier
	WithJobQueue                      = service.WithJobQueue
	WithOutcomeRecorder               = service.WithOutcomeRecorder
	WithProvenMaxBitErrorRate         = service.WithProvenMaxBitErrorRate
	WithSchedulerEvents               = service.WithSchedulerEvents
	WithSchedulerPaused               = service.WithSchedulerPaused
	WithSupersededAudioGrace          = service.WithSupersededAudioGrace
	WithUpgradeLane                   = service.WithUpgradeLane
	WithVerificationStatus            = service.WithVerificationStatus
	WithVerifySkipCount               = service.WithVerifySkipCount
)

type ConcurrencyLimit = service.ConcurrencyLimit

const (
	VerdictContradicted  = service.VerdictContradicted
	VerdictDifferentSong = service.VerdictDifferentSong
	VerdictHard          = service.VerdictHard
	VerdictOtherVersion  = service.VerdictOtherVersion
	VerdictSoft          = service.VerdictSoft
	VerdictUnknown       = service.VerdictUnknown
)

var ErrInPlaceUnavailable = service.ErrInPlaceUnavailable

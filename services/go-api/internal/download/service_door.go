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
	AudioVerdictOf                        = service.AudioVerdictOf
	BuildLegacyAudioRef                   = service.BuildLegacyAudioRef
	BuildLegacyAudioRefUncapped           = service.BuildLegacyAudioRefUncapped
	FingerprintOf                         = service.FingerprintOf
	NewAcquireTrackAudioService           = service.NewAcquireTrackAudioService
	NewBackgroundAcquisitionScheduler     = service.NewBackgroundAcquisitionScheduler
	NewReacquireAdmission                 = service.NewReacquireAdmission
	NewRetryAdmission                     = service.NewRetryAdmission
	NewSourceRegistry                     = service.NewSourceRegistry
	ScoreConfidence                       = service.ScoreConfidence
	SourceKindOf                          = service.SourceKindOf
	DirectDeleteOrphans                   = service.DirectDeleteOrphans
	MustDirectDeleteOrphans               = service.MustDirectDeleteOrphans
	MustNewAcquireTrackAudioService       = service.MustNewAcquireTrackAudioService
	MustNewBackgroundAcquisitionScheduler = service.MustNewBackgroundAcquisitionScheduler
	ErrLimitBelowOne                      = service.ErrLimitBelowOne
	ErrUpgradeWorkersBelowOne             = service.ErrUpgradeWorkersBelowOne
	ErrLimitNotConfigured                 = service.ErrLimitNotConfigured
	ErrMissingCollaborator                = service.ErrMissingCollaborator
	InMemoryQueue                         = service.InMemoryQueue
	QueueOf                               = service.QueueOf
	UpgradeLaneOf                         = service.UpgradeLaneOf
	MustUpgradeLaneOf                     = service.MustUpgradeLaneOf
	NoUpgradeLane                         = service.NoUpgradeLane
	NoObserver                            = service.NoObserver
	LimitedTo                             = service.LimitedTo
	MustLimitedTo                         = service.MustLimitedTo
	Unlimited                             = service.Unlimited
	SweepStaleTempDirs                    = service.SweepStaleTempDirs
	UploaderSourceKind                    = service.UploaderSourceKind
	WithAcquireConfidenceFloor            = service.WithAcquireConfidenceFloor
	WithAcquireStoreKeyPrefix             = service.WithAcquireStoreKeyPrefix
	WithDrainBudget                       = service.WithDrainBudget
	WithProvenMaxBitErrorRate             = service.WithProvenMaxBitErrorRate
	WithSchedulerPaused                   = service.WithSchedulerPaused
	WithSupersededAudioGrace              = service.WithSupersededAudioGrace
	WithVerificationStatus                = service.WithVerificationStatus
)

type ConcurrencyLimit = service.ConcurrencyLimit

type SchedulerPorts = service.SchedulerPorts

type UpgradeLane = service.UpgradeLane

const (
	VerdictContradicted  = service.VerdictContradicted
	VerdictDifferentSong = service.VerdictDifferentSong
	VerdictHard          = service.VerdictHard
	VerdictOtherVersion  = service.VerdictOtherVersion
	VerdictSoft          = service.VerdictSoft
	VerdictUnknown       = service.VerdictUnknown
)

var ErrInPlaceUnavailable = service.ErrInPlaceUnavailable

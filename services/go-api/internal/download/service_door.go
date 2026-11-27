package download

import (
	"altune/go-api/internal/download/internal/core/ports"
	service "altune/go-api/internal/download/service"
)

type (
	AcquisitionJob                 = ports.AcquisitionJob
	AcquireTrackAudioService       = service.AcquireTrackAudioService
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	ReplaceService                 = service.ReplaceService
	StagePorts                     = service.StagePorts
	VerificationPorts              = service.VerificationPorts
	VerifySkipCounter              = service.VerifySkipCounter
)

var (
	NewReacquireAdmission                 = service.NewReacquireAdmission
	NewRetryAdmission                     = service.NewRetryAdmission
	NewReplaceService                     = service.NewReplaceService
	NewSourceRegistry                     = service.NewSourceRegistry
	MustDirectDeleteOrphans               = service.MustDirectDeleteOrphans
	MustNewAcquireTrackAudioService       = service.MustNewAcquireTrackAudioService
	MustNewBackgroundAcquisitionScheduler = service.MustNewBackgroundAcquisitionScheduler
	InMemoryQueue                         = service.InMemoryQueue
	QueueOf                               = service.QueueOf
	MustUpgradeLaneOf                     = service.MustUpgradeLaneOf
	NoUpgradeLane                         = service.NoUpgradeLane
	NoObserver                            = service.NoObserver
	MustLimitedTo                         = service.MustLimitedTo
	Unlimited                             = service.Unlimited
	SweepStaleTempDirs                    = service.SweepStaleTempDirs
	WithAcquireConfidenceFloor            = service.WithAcquireConfidenceFloor
	WithAcquireStoreKeyPrefix             = service.WithAcquireStoreKeyPrefix
	WithDrainBudget                       = service.WithDrainBudget
	WithProvenMaxBitErrorRate             = service.WithProvenMaxBitErrorRate
	WithSchedulerPaused                   = service.WithSchedulerPaused
	WithSupersededAudioGrace              = service.WithSupersededAudioGrace
)

type SchedulerPorts = service.SchedulerPorts

type UpgradeLane = service.UpgradeLane

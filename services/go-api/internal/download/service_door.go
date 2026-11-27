package download

import (
	service "altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/jobrun"
)

type (
	AcquisitionJob                 = jobrun.Job
	AcquireTrackAudioService       = service.AcquireTrackAudioService
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	StagePorts                     = service.StagePorts
	VerificationPorts              = service.VerificationPorts
	VerifySkipCounter              = service.VerifySkipCounter
)

var (
	NewReacquireAdmission                 = service.NewReacquireAdmission
	NewRetryAdmission                     = service.NewRetryAdmission
	NewSourceRegistry                     = service.NewSourceRegistry
	MustDirectDeleteOrphans               = service.MustDirectDeleteOrphans
	MustNewAcquireTrackAudioService       = service.MustNewAcquireTrackAudioService
	MustNewBackgroundAcquisitionScheduler = service.MustNewBackgroundAcquisitionScheduler
	NewProcessRoot                        = service.NewProcessRoot
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

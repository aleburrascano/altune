package download

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
)

type (
	AcquisitionJob                 = ports.AcquisitionJob
	AcquireTrackAudioService       = service.AcquireTrackAudioService
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	InPlaceChecker                 = service.InPlaceChecker
	StagePorts                     = service.StagePorts
	VerificationPorts              = service.VerificationPorts
)

var (
	NewReacquireAdmission                 = service.NewReacquireAdmission
	NewRetryAdmission                     = service.NewRetryAdmission
	NewInPlaceChecker                     = service.NewInPlaceChecker
	NewReplaceService                     = service.NewReplaceService
	NewSourceRegistry                     = service.NewSourceRegistry
	MustDirectDeleteOrphans               = service.MustDirectDeleteOrphans
	MustNewAcquireTrackAudioService       = service.MustNewAcquireTrackAudioService
	MustNewBackgroundAcquisitionScheduler = service.MustNewBackgroundAcquisitionScheduler
	QueueOf                               = service.QueueOf
	NoUpgradeLane                         = service.NoUpgradeLane
	NoObserver                            = service.NoObserver
	MustLimitedTo                         = service.MustLimitedTo
	Unlimited                             = service.Unlimited
	SweepStaleTempDirs                    = service.SweepStaleTempDirs
	WithProvenMaxBitErrorRate             = service.WithProvenMaxBitErrorRate
)

type SchedulerPorts = service.SchedulerPorts

package download

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
)

type (
	AcquisitionJob                 = ports.AcquisitionJob
	BackgroundAcquisitionScheduler = service.BackgroundAcquisitionScheduler
	StagePorts                     = service.StagePorts
	VerificationPorts              = service.VerificationPorts
)

var (
	NewReacquireAdmission           = service.NewReacquireAdmission
	NewRetryAdmission               = service.NewRetryAdmission
	NewSourceRegistry               = service.NewSourceRegistry
	MustDirectDeleteOrphans         = service.MustDirectDeleteOrphans
	MustNewAcquireTrackAudioService = service.MustNewAcquireTrackAudioService
	NoObserver                      = service.NoObserver
	Unlimited                       = service.Unlimited
	SweepStaleTempDirs              = service.SweepStaleTempDirs
)

package download

import (
	domain "altune/go-api/internal/download/internal/core/domain"
	upgrade "altune/go-api/internal/download/service/replacement"
)

var (
	ErrAcquisitionPaused = domain.ErrAcquisitionPaused
	ErrCooldownActive    = domain.ErrCooldownActive
	ErrLeaseLost         = domain.ErrLeaseLost
	ErrReacquireNotReady = domain.ErrReacquireNotReady
	ErrRetryNotFailed    = domain.ErrRetryNotFailed
	ErrSchedulerShutdown = domain.ErrSchedulerShutdown
	ErrTrackJobInFlight  = domain.ErrTrackJobInFlight
)

var (
	ErrNotAnUpgrade = upgrade.ErrNotAnUpgrade
	ErrUpgradeKept  = upgrade.ErrKept
)

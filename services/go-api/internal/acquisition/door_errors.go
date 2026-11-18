package acquisition

import (
	domain "altune/go-api/internal/acquisition/domain"
	upgrade "altune/go-api/internal/acquisition/service/upgrade"
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
	ErrWouldUpgrade = upgrade.ErrWouldUpgrade
)

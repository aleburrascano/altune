package domain

import shareddomain "altune/go-api/internal/shared/domain"

var (
	ErrRetryNotFailed    = shareddomain.NewError(shareddomain.CodeAcquisitionRetryNotFailed, "track is not in failed state")
	ErrCooldownActive    = shareddomain.NewError(shareddomain.CodeAcquisitionCooldownActive, "cooldown active, try again later")
	ErrReacquireNotReady = shareddomain.NewError(shareddomain.CodeAcquisitionReacquireNotReady, "track has no audio to replace")
)

var ErrTrackJobInFlight = shareddomain.NewError(shareddomain.CodeAcquisitionJobInFlight,
	"another acquisition for this track is already running, try again later")

var ErrSchedulerShutdown = shareddomain.NewError(shareddomain.CodeAcquisitionShuttingDown,
	"acquisition is shutting down, try again later")

var ErrAcquisitionPaused = shareddomain.NewError(shareddomain.CodeAcquisitionPaused,
	"acquisition is paused, try again later")

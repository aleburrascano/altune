package domain

import apierror "altune/go-api/internal/shared/apierror"

var (
	ErrRetryNotFailed    = apierror.NewError(apierror.CodeAcquisitionRetryNotFailed, "track is not in failed state")
	ErrCooldownActive    = apierror.NewError(apierror.CodeAcquisitionCooldownActive, "cooldown active, try again later")
	ErrReacquireNotReady = apierror.NewError(apierror.CodeAcquisitionReacquireNotReady, "track has no audio to replace")
)

var ErrTrackJobInFlight = apierror.NewError(apierror.CodeAcquisitionJobInFlight,
	"another acquisition for this track is already running, try again later")

var ErrSchedulerShutdown = apierror.NewError(apierror.CodeAcquisitionShuttingDown,
	"acquisition is shutting down, try again later")

var ErrAcquisitionPaused = apierror.NewError(apierror.CodeAcquisitionPaused,
	"acquisition is paused, try again later")

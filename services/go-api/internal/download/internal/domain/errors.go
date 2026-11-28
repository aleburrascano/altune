package domain

import (
	"errors"
	"fmt"
)

var (
	ErrRetryNotFailed    = errors.New("track is not in failed state")
	ErrCooldownActive    = errors.New("cooldown active, try again later")
	ErrReacquireNotReady = errors.New("track has no audio to replace")
	ErrTrackJobInFlight  = errors.New("another acquisition for this track is already running, try again later")
	ErrSchedulerShutdown = errors.New("acquisition is shutting down, try again later")
	ErrAcquisitionPaused = errors.New("acquisition is paused, try again later")
)

var (
	ErrLeaseLost        = errors.New("acquisition lease lost")
	ErrNoJobAvailable   = errors.New("no acquisition job available")
	ErrJobKindConflict  = errors.New("acquisition job of a different kind is already in flight")
	ErrSearchDegraded   = errors.New("search degraded: a query or source failed and nothing was found")
	ErrEmptyFingerprint = errors.New("fpcalc: empty fingerprint")

	ErrIdentifyThrottled = errors.New("acoustid: throttled")

	ErrIllegalTransition = errors.New("illegal acquisition status transition")
	ErrTrackNotFound     = errors.New("track not found")

	ErrNoAudioIdentifier   = errors.New("no audio identifier is wired")
	ErrNoAudioProber       = errors.New("no audio prober is wired")
	ErrNoStoredAudioReader = errors.New("no stored-audio reader is wired")
)

type SourceUnavailableError struct {
	Source SourceName
	Err    error
}

func (e *SourceUnavailableError) Error() string {
	return fmt.Sprintf("source %s unavailable: %v", e.Source, e.Err)
}

func (e *SourceUnavailableError) Unwrap() error { return e.Err }

func IsSourceUnavailable(err error) bool {
	var unavailable *SourceUnavailableError
	return errors.As(err, &unavailable)
}

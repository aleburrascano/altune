package service

type admissionError struct {
	msg    string
	status int
	code   string
}

func (e *admissionError) Error() string     { return e.msg }
func (e *admissionError) HTTPStatus() int   { return e.status }
func (e *admissionError) ErrorCode() string { return e.code }

var (
	ErrRetryNotFailed = &admissionError{
		msg:    "track is not in failed state",
		status: 409,
		code:   "acquisition.retry_not_failed",
	}
	ErrCooldownActive = &admissionError{
		msg:    "cooldown active, try again later",
		status: 429,
		code:   "acquisition.cooldown_active",
	}
	ErrReacquireNotReady = &admissionError{
		msg:    "track has no audio to replace",
		status: 409,
		code:   "acquisition.reacquire_not_ready",
	}
)

var ErrAcquisitionQueueFull = &admissionError{
	msg:    "acquisition queue is full, try again later",
	status: 503,
	code:   "acquisition.queue_full",
}

var ErrPrincipalQueueFull = &admissionError{
	msg:    "too many concurrent acquisitions for this user, try again later",
	status: 429,
	code:   "acquisition.principal_queue_full",
}

var ErrTrackJobInFlight = &admissionError{
	msg:    "another acquisition for this track is already running, try again later",
	status: 409,
	code:   "acquisition.job_in_flight",
}

var ErrSchedulerShutdown = &admissionError{
	msg:    "acquisition is shutting down, try again later",
	status: 503,
	code:   "acquisition.shutting_down",
}

var ErrAcquisitionPaused = &admissionError{
	msg:    "acquisition is paused, try again later",
	status: 503,
	code:   "acquisition.paused",
}

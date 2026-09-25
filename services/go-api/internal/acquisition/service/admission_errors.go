package service

// admissionError carries a stable error code and HTTP status so admission
// sentinels route through httputil.HandleServiceError like other handlers. The
// literal statuses avoid importing net/http, which the application layer forbids.
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

// ErrAcquisitionQueueFull reports that the bounded admission queue shed the
// job: nothing was queued, so the caller must not treat the request as accepted.
var ErrAcquisitionQueueFull = &admissionError{
	msg:    "acquisition queue is full, try again later",
	status: 503,
	code:   "acquisition.queue_full",
}

// ErrPrincipalQueueFull reports that the caller (userId) already holds its
// per-principal share of the admission queue: nothing was queued for this
// request, but slots remain for other principals. Retryable once the
// principal's in-flight jobs drain.
var ErrPrincipalQueueFull = &admissionError{
	msg:    "too many concurrent acquisitions for this user, try again later",
	status: 429,
	code:   "acquisition.principal_queue_full",
}

// ErrTrackJobInFlight reports that a job of the other kind holds the track's
// in-flight slot, so the requested one was not queued: a replace cannot run
// while a plain acquisition does, and neither stands in for the other.
// Retryable once the running job settles.
var ErrTrackJobInFlight = &admissionError{
	msg:    "another acquisition for this track is already running, try again later",
	status: 409,
	code:   "acquisition.job_in_flight",
}

// ErrSchedulerShutdown reports that the scheduler is draining and refused the job.
var ErrSchedulerShutdown = &admissionError{
	msg:    "acquisition is shutting down, try again later",
	status: 503,
	code:   "acquisition.shutting_down",
}

// ErrAcquisitionPaused reports that acquisition has been paused at runtime (a
// kill switch, distinct from process shutdown): nothing was queued, but the
// job admits again once Resume re-enables the scheduler
// without a process restart. In-flight jobs are unaffected by the pause.
var ErrAcquisitionPaused = &admissionError{
	msg:    "acquisition is paused, try again later",
	status: 503,
	code:   "acquisition.paused",
}

package domain

type StaleWriteError struct {
	msg  string
	code string
}

func (e *StaleWriteError) Error() string     { return e.msg }
func (e *StaleWriteError) HTTPStatus() int   { return 409 }
func (e *StaleWriteError) ErrorCode() string { return e.code }

var ErrStaleQueueWrite error = &StaleWriteError{
	msg:  "queue state not saved: a newer snapshot is already stored",
	code: "playback.stale_queue_write",
}

var ErrQueuePositionMismatch error = &StaleWriteError{
	msg:  "queue position not saved: the stored queue does not hold that track at that index",
	code: "playback.queue_position_mismatch",
}

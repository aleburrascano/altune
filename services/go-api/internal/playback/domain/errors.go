package domain

// StaleWriteError is a classified conflict: it carries its HTTP status and a
// stable machine-readable code so httputil.HandleServiceError can surface it.
type StaleWriteError struct {
	msg  string
	code string
}

func (e *StaleWriteError) Error() string     { return e.msg }
func (e *StaleWriteError) HTTPStatus() int   { return 409 }
func (e *StaleWriteError) ErrorCode() string { return e.code }

// ErrStaleQueueWrite reports that a queue-state save was rejected because the
// stored snapshot is newer than the one being written (last-write-wins by
// updated_at). Nothing was persisted. Match it with errors.Is.
var ErrStaleQueueWrite error = &StaleWriteError{
	msg:  "queue state not saved: a newer snapshot is already stored",
	code: "playback.stale_queue_write",
}

// ErrQueuePositionMismatch reports that a position-only save was not applied
// because no stored queue holds its CurrentTrackId at its CurrentIdx (nothing
// is stored, or the stored queue is a different one). Nothing was persisted;
// the client should send a full queue-state save instead. Match it with
// errors.Is.
var ErrQueuePositionMismatch error = &StaleWriteError{
	msg:  "queue position not saved: the stored queue does not hold that track at that index",
	code: "playback.queue_position_mismatch",
}

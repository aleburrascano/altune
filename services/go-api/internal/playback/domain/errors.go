package domain

import "altune/go-api/internal/shared"

// ValidationError aliases the shared type so this package keeps one name for
// its 400s while the implementation lives in internal/shared.
type ValidationError = shared.ValidationError

// The code a rejected save carries, one per constraint, so a client branches on
// which rule it broke instead of parsing the detail text (#1596).
const (
	codePositionMsNegative    = "playback.position_ms_negative"
	codeQueueTooLong          = "playback.queue_too_long"
	codeStringTooLong         = "playback.string_too_long"
	codeStringContainsNul     = "playback.string_contains_nul"
	codeCurrentIdxOutOfRange  = "playback.current_idx_out_of_range"
	codeCurrentTrackIdMissing = "playback.current_track_id_missing"
	codeUnknownRepeatMode     = "playback.unknown_repeat_mode"
	codeUnknownSourceKind     = "playback.unknown_source_kind"
	codeUnrecognizedSourceId  = "playback.unrecognized_source_id"
)

// QueueValidationError is a 400 classified by cause. It carries a
// *ValidationError rather than replacing it, so the service and persistence
// layers that ask errors.As whether a save was rejected still get their answer
// while the wire code names the constraint that rejected it.
type QueueValidationError struct {
	code  string
	cause *ValidationError
}

func (e *QueueValidationError) Error() string     { return e.cause.Error() }
func (e *QueueValidationError) Unwrap() error     { return e.cause }
func (e *QueueValidationError) HTTPStatus() int   { return e.cause.HTTPStatus() }
func (e *QueueValidationError) ErrorCode() string { return e.code }

func newValidationError(code, msg string) *QueueValidationError {
	return &QueueValidationError{code: code, cause: shared.NewValidationError("playback", msg)}
}

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

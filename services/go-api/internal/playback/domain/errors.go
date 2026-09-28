package domain

import "altune/go-api/internal/shared"

type ValidationError = shared.ValidationError

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

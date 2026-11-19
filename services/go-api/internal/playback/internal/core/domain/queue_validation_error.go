package domain

import shareddomain "altune/go-api/internal/shared/domain"

type ValidationError = shareddomain.ValidationError

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
	codeMalformedTrackId      = "playback.malformed_track_id"
	codeNaturalOrderMismatch  = "playback.natural_order_mismatch"
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
	return &QueueValidationError{code: code, cause: shareddomain.NewValidationError("playback", msg)}
}

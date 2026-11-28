package domain

import (
	"errors"
)

var (
	ErrStaleQueueWrite       = errors.New("queue state not saved: a newer snapshot is already stored")
	ErrQueuePositionMismatch = errors.New("queue position not saved: the stored queue does not hold that track at that index")
	ErrQueueStateUnavailable = errors.New("queue state temporarily unavailable")
	ErrCorruptStoredState    = errors.New("corrupt stored queue state")
	ErrQueueStateNotFound    = errors.New("no stored queue state")

	ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

	ErrTrackSourceUnavailable  = errors.New("now-playing track source unavailable")
	ErrNowPlayingTrackNotFound = errors.New("now-playing track not found")
	ErrEnrichmentUnavailable   = errors.New("now-playing enrichment temporarily unavailable")

	ErrCollaboratorRequired = errors.New("nowplaying: a required collaborator is missing")
	ErrClockRequired        = errors.New("nowplaying: the reader requires a clock")
	ErrInvalidTimeout       = errors.New("nowplaying: the lookup timeout must be positive")
)

type ValidationKind string

const (
	KindPositionMsNegative    ValidationKind = "position_ms_negative"
	KindQueueTooLong          ValidationKind = "queue_too_long"
	KindStringTooLong         ValidationKind = "string_too_long"
	KindStringContainsNul     ValidationKind = "string_contains_nul"
	KindCurrentIdxOutOfRange  ValidationKind = "current_idx_out_of_range"
	KindCurrentTrackIdMissing ValidationKind = "current_track_id_missing"
	KindUnknownRepeatMode     ValidationKind = "unknown_repeat_mode"
	KindUnknownSourceKind     ValidationKind = "unknown_source_kind"
	KindUnrecognizedSourceId  ValidationKind = "unrecognized_source_id"
	KindMalformedTrackId      ValidationKind = "malformed_track_id"
	KindNaturalOrderMismatch  ValidationKind = "natural_order_mismatch"
)

func ValidationKinds() []ValidationKind {
	return []ValidationKind{
		KindPositionMsNegative, KindQueueTooLong, KindStringTooLong, KindStringContainsNul,
		KindCurrentIdxOutOfRange, KindCurrentTrackIdMissing, KindUnknownRepeatMode,
		KindUnknownSourceKind, KindUnrecognizedSourceId, KindMalformedTrackId, KindNaturalOrderMismatch,
	}
}

type ValidationError struct {
	Kind ValidationKind
	msg  string
}

func newValidationError(kind ValidationKind, msg string) *ValidationError {
	return &ValidationError{Kind: kind, msg: msg}
}

func (e *ValidationError) Error() string { return e.msg }

func (e *ValidationError) Is(target error) bool {
	other, ok := target.(*ValidationError)
	return ok && other != nil && other.Kind == e.Kind
}

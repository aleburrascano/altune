package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"
)

// ValidationError is a client-caused construction failure. It structurally
// implements httputil.StatusError (the status is a plain int so the domain
// layer stays free of net/http), so handlers map it to 400 via
// httputil.HandleServiceError instead of a blanket 500.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string   { return e.Message }
func (e *ValidationError) HTTPStatus() int { return 400 }

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

func (r RepeatMode) String() string {
	switch r {
	case RepeatOff:
		return "off"
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return "off"
	}
}

func ParseRepeatMode(s string) (RepeatMode, error) {
	switch s {
	case "off", "":
		return RepeatOff, nil
	case "all":
		return RepeatAll, nil
	case "one":
		return RepeatOne, nil
	default:
		return RepeatOff, &ValidationError{Message: fmt.Sprintf("unknown repeat mode: %q", s)}
	}
}

// QueueState is the server-persisted snapshot of a user's playback Queue, saved
// for resume-on-reopen. The live queue (advance/prev/shuffle/repeat) is
// client-owned per ADR-0010; this is only the snapshot.
//
// AIDEV-DECISION: TrackIds is []string, not a TrackId value object. The catalog
// context owns TrackId identity; playback references those tracks by id across
// the context seam (reference-by-id, a standard DDD pattern). Wrapping them here
// would couple playback to catalog for a snapshot the server never reasons about
// domain-wise. Kept as strings deliberately.
type QueueState struct {
	UserId     shared.UserId
	TrackIds   []string
	CurrentIdx int
	PositionMs int64
	Shuffled   bool
	RepeatMode RepeatMode
	// SourceId identifies what the queue was built from (e.g. playlist ID, "library", "search").
	SourceId string
	// NaturalOrder is the same tracks as TrackIds but in their pre-shuffle
	// (album/playlist/library) order. TrackIds is play order; NaturalOrder lets the
	// client rebuild the exact shuffled sequence AND un-shuffle back to the original
	// order after relaunch. Opaque to the server — carried through, never reasoned
	// over. Empty for older rows / clients that don't send it.
	NaturalOrder []string
	UpdatedAt    time.Time
}

// QueueStateInput carries the fields a QueueState is constructed from. Grouping
// them in one struct removes the transposition hazard of a long positional
// argument list (TrackIds and NaturalOrder are both []string, so swapping them
// would compile silently) and localizes future field additions to one place.
type QueueStateInput struct {
	UserId       shared.UserId
	TrackIds     []string
	CurrentIdx   int
	PositionMs   int64
	Shuffled     bool
	RepeatMode   RepeatMode
	SourceId     string
	NaturalOrder []string
}

// newQueueState is the single door every QueueState passes through. Empty queues
// normalize CurrentIdx to 0; non-empty queues must index in range. Used by both
// fresh construction and storage reconstitution so the invariant has one home.
func newQueueState(in QueueStateInput, updatedAt time.Time) (*QueueState, error) {
	if in.PositionMs < 0 {
		return nil, &ValidationError{Message: fmt.Sprintf("positionMs must be non-negative, got %d", in.PositionMs)}
	}
	// Normalize nil slices (NULL/empty array from storage, omitted JSON field)
	// to empty so TrackIds and NaturalOrder are never nil — callers and JSON
	// serialization can rely on it. This is the single home for the invariant.
	trackIds := in.TrackIds
	if trackIds == nil {
		trackIds = []string{}
	}
	naturalOrder := in.NaturalOrder
	if naturalOrder == nil {
		naturalOrder = []string{}
	}
	currentIdx := in.CurrentIdx
	if len(trackIds) == 0 {
		currentIdx = 0
	} else if currentIdx < 0 || currentIdx >= len(trackIds) {
		return nil, &ValidationError{Message: fmt.Sprintf("currentIdx %d out of range [0, %d)", currentIdx, len(trackIds))}
	}
	return &QueueState{
		UserId:       in.UserId,
		TrackIds:     trackIds,
		CurrentIdx:   currentIdx,
		PositionMs:   in.PositionMs,
		Shuffled:     in.Shuffled,
		RepeatMode:   in.RepeatMode,
		SourceId:     in.SourceId,
		NaturalOrder: naturalOrder,
		UpdatedAt:    updatedAt,
	}, nil
}

// NewQueueState builds a fresh snapshot, stamping UpdatedAt to now.
func NewQueueState(in QueueStateInput) (*QueueState, error) {
	return newQueueState(in, time.Now().UTC())
}

// RehydrateQueueState reconstitutes a snapshot read from storage, preserving its
// stored UpdatedAt. It enforces the same invariants as NewQueueState so a corrupt
// row cannot reconstitute into an invalid QueueState.
func RehydrateQueueState(in QueueStateInput, updatedAt time.Time) (*QueueState, error) {
	return newQueueState(in, updatedAt)
}

// EmptyQueueState is the canonical "no queue" snapshot for a user — the single
// definition of what an absent queue looks like. It routes through the same
// newQueueState gate as every other constructor so the "single door" invariant
// has no exception; empty input cannot fail validation, so the error is discarded.
func EmptyQueueState(userId shared.UserId) *QueueState {
	state, _ := newQueueState(QueueStateInput{UserId: userId}, time.Now().UTC())
	return state
}

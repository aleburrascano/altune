package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"
)

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
		return RepeatOff, fmt.Errorf("unknown repeat mode: %q", s)
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

// QueueStateOption configures optional snapshot fields (functional options — the
// house constructor idiom). Appended variadically so the positional constructors
// stay source-compatible.
type QueueStateOption func(*QueueState)

// WithNaturalOrder attaches the pre-shuffle track order to the snapshot.
func WithNaturalOrder(naturalOrder []string) QueueStateOption {
	return func(s *QueueState) {
		if naturalOrder == nil {
			naturalOrder = []string{}
		}
		s.NaturalOrder = naturalOrder
	}
}

// newQueueState is the single door every QueueState passes through. Empty queues
// normalize CurrentIdx to 0; non-empty queues must index in range. Used by both
// fresh construction and storage reconstitution so the invariant has one home.
func newQueueState(
	userId shared.UserId,
	trackIds []string,
	currentIdx int,
	positionMs int64,
	shuffled bool,
	repeatMode RepeatMode,
	sourceId string,
	updatedAt time.Time,
	opts ...QueueStateOption,
) (*QueueState, error) {
	if positionMs < 0 {
		return nil, fmt.Errorf("positionMs must be non-negative, got %d", positionMs)
	}
	// Normalize a nil slice (NULL/empty array from storage, omitted JSON field)
	// to empty so TrackIds is never nil — callers and JSON serialization can rely
	// on it. This is the single home for the invariant.
	if trackIds == nil {
		trackIds = []string{}
	}
	if len(trackIds) == 0 {
		currentIdx = 0
	} else if currentIdx < 0 || currentIdx >= len(trackIds) {
		return nil, fmt.Errorf("currentIdx %d out of range [0, %d)", currentIdx, len(trackIds))
	}
	state := &QueueState{
		UserId:       userId,
		TrackIds:     trackIds,
		CurrentIdx:   currentIdx,
		PositionMs:   positionMs,
		Shuffled:     shuffled,
		RepeatMode:   repeatMode,
		SourceId:     sourceId,
		NaturalOrder: []string{},
		UpdatedAt:    updatedAt,
	}
	for _, opt := range opts {
		opt(state)
	}
	return state, nil
}

// NewQueueState builds a fresh snapshot, stamping UpdatedAt to now.
func NewQueueState(
	userId shared.UserId,
	trackIds []string,
	currentIdx int,
	positionMs int64,
	shuffled bool,
	repeatMode RepeatMode,
	sourceId string,
	opts ...QueueStateOption,
) (*QueueState, error) {
	return newQueueState(userId, trackIds, currentIdx, positionMs, shuffled, repeatMode, sourceId, time.Now().UTC(), opts...)
}

// RehydrateQueueState reconstitutes a snapshot read from storage, preserving its
// stored UpdatedAt. It enforces the same invariants as NewQueueState so a corrupt
// row cannot reconstitute into an invalid QueueState.
func RehydrateQueueState(
	userId shared.UserId,
	trackIds []string,
	currentIdx int,
	positionMs int64,
	shuffled bool,
	repeatMode RepeatMode,
	sourceId string,
	updatedAt time.Time,
	opts ...QueueStateOption,
) (*QueueState, error) {
	return newQueueState(userId, trackIds, currentIdx, positionMs, shuffled, repeatMode, sourceId, updatedAt, opts...)
}

// EmptyQueueState is the canonical "no queue" snapshot for a user — the single
// definition of what an absent queue looks like.
func EmptyQueueState(userId shared.UserId) *QueueState {
	return &QueueState{
		UserId:       userId,
		TrackIds:     []string{},
		CurrentIdx:   0,
		PositionMs:   0,
		Shuffled:     false,
		RepeatMode:   RepeatOff,
		NaturalOrder: []string{},
		UpdatedAt:    time.Now().UTC(),
	}
}

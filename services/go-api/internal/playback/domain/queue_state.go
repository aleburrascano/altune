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
	SourceId  string
	UpdatedAt time.Time
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
) (*QueueState, error) {
	if positionMs < 0 {
		return nil, fmt.Errorf("positionMs must be non-negative, got %d", positionMs)
	}
	if len(trackIds) == 0 {
		currentIdx = 0
	} else if currentIdx < 0 || currentIdx >= len(trackIds) {
		return nil, fmt.Errorf("currentIdx %d out of range [0, %d)", currentIdx, len(trackIds))
	}
	return &QueueState{
		UserId:     userId,
		TrackIds:   trackIds,
		CurrentIdx: currentIdx,
		PositionMs: positionMs,
		Shuffled:   shuffled,
		RepeatMode: repeatMode,
		SourceId:   sourceId,
		UpdatedAt:  updatedAt,
	}, nil
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
) (*QueueState, error) {
	return newQueueState(userId, trackIds, currentIdx, positionMs, shuffled, repeatMode, sourceId, time.Now().UTC())
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
) (*QueueState, error) {
	return newQueueState(userId, trackIds, currentIdx, positionMs, shuffled, repeatMode, sourceId, updatedAt)
}

// EmptyQueueState is the canonical "no queue" snapshot for a user — the single
// definition of what an absent queue looks like.
func EmptyQueueState(userId shared.UserId) *QueueState {
	return &QueueState{
		UserId:     userId,
		TrackIds:   []string{},
		CurrentIdx: 0,
		PositionMs: 0,
		Shuffled:   false,
		RepeatMode: RepeatOff,
		UpdatedAt:  time.Now().UTC(),
	}
}

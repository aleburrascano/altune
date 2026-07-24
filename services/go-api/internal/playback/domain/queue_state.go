package domain

import (
	"fmt"
	"time"

	"altune/go-api/internal/shared"
)

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

type QueueState struct {
	UserId       shared.UserId
	TrackIds     []string
	CurrentIdx   int
	PositionMs   int64
	Shuffled     bool
	RepeatMode   RepeatMode
	SourceId     string
	NaturalOrder []string
	UpdatedAt    time.Time
}

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

func newQueueState(in QueueStateInput, updatedAt time.Time) (*QueueState, error) {
	if in.PositionMs < 0 {
		return nil, &ValidationError{Message: fmt.Sprintf("positionMs must be non-negative, got %d", in.PositionMs)}
	}
	trackIds := emptyIfNil(in.TrackIds)
	currentIdx, err := indexWithinQueue(in.CurrentIdx, len(trackIds))
	if err != nil {
		return nil, err
	}
	return &QueueState{
		UserId:       in.UserId,
		TrackIds:     trackIds,
		CurrentIdx:   currentIdx,
		PositionMs:   in.PositionMs,
		Shuffled:     in.Shuffled,
		RepeatMode:   in.RepeatMode,
		SourceId:     in.SourceId,
		NaturalOrder: emptyIfNil(in.NaturalOrder),
		UpdatedAt:    updatedAt,
	}, nil
}

func emptyIfNil(trackIds []string) []string {
	if trackIds == nil {
		return []string{}
	}
	return trackIds
}

func indexWithinQueue(currentIdx, queueLen int) (int, error) {
	if queueLen == 0 {
		return 0, nil
	}
	if currentIdx < 0 || currentIdx >= queueLen {
		return 0, &ValidationError{Message: fmt.Sprintf("currentIdx %d out of range [0, %d)", currentIdx, queueLen)}
	}
	return currentIdx, nil
}

func NewQueueState(in QueueStateInput) (*QueueState, error) {
	return newQueueState(in, time.Now().UTC())
}

func RehydrateQueueState(in QueueStateInput, updatedAt time.Time) (*QueueState, error) {
	return newQueueState(in, updatedAt)
}

func EmptyQueueState(userId shared.UserId) *QueueState {
	state, err := newQueueState(QueueStateInput{UserId: userId}, time.Now().UTC())
	if err != nil {
		panic("empty queue state must always be valid: " + err.Error())
	}
	return state
}

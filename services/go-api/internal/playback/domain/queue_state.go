package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"strings"
	"time"
)

const MaxQueueLength = 10000

const MaxQueueStringBytes = 4096

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
	trackIds := emptyIfNil(in.TrackIds)
	naturalOrder := emptyIfNil(in.NaturalOrder)
	if err := checkQueueInvariants(queueInvariantFields{
		PositionMs:   in.PositionMs,
		TrackIds:     trackIds,
		NaturalOrder: naturalOrder,
		SourceId:     in.SourceId,
		CurrentIdx:   in.CurrentIdx,
	}); err != nil {
		return nil, err
	}
	currentIdx, _ := indexWithinQueue(in.CurrentIdx, len(trackIds))
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

func (q *QueueState) Validate() error {
	if err := checkQueueInvariants(queueInvariantFields{
		PositionMs:   q.PositionMs,
		TrackIds:     q.TrackIds,
		NaturalOrder: q.NaturalOrder,
		SourceId:     q.SourceId,
		CurrentIdx:   q.CurrentIdx,
	}); err != nil {
		return err
	}
	return q.currentTrackIdPresent()
}

func (q *QueueState) currentTrackIdPresent() error {
	if id, hasCurrent := q.CurrentTrackId(); hasCurrent && id == "" {
		return newValidationError(codeCurrentTrackIdMissing, "trackIds[currentIdx] must be a non-empty track id")
	}
	return nil
}

type queueInvariantFields struct {
	PositionMs   int64
	TrackIds     []string
	NaturalOrder []string
	SourceId     string
	CurrentIdx   int
}

func checkQueueInvariants(fields queueInvariantFields) error {
	if fields.PositionMs < 0 {
		return newValidationError(codePositionMsNegative, fmt.Sprintf("positionMs must be non-negative, got %d", fields.PositionMs))
	}
	if err := lengthWithinBound("trackIds", len(fields.TrackIds)); err != nil {
		return err
	}
	if err := lengthWithinBound("naturalOrder", len(fields.NaturalOrder)); err != nil {
		return err
	}
	if err := elementsStorable("trackIds", fields.TrackIds); err != nil {
		return err
	}
	if err := elementsStorable("naturalOrder", fields.NaturalOrder); err != nil {
		return err
	}
	if err := stringStorable("sourceId", fields.SourceId); err != nil {
		return err
	}
	if _, err := indexWithinQueue(fields.CurrentIdx, len(fields.TrackIds)); err != nil {
		return err
	}
	return nil
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func elementsStorable(field string, values []string) error {
	for _, value := range values {
		if err := stringStorable(field, value); err != nil {
			return err
		}
	}
	return nil
}

func stringStorable(field, value string) error {
	if len(value) > MaxQueueStringBytes {
		return newValidationError(codeStringTooLong, fmt.Sprintf("%s length %d bytes exceeds maximum %d", field, len(value), MaxQueueStringBytes))
	}
	if strings.IndexByte(value, 0) >= 0 {
		return newValidationError(codeStringContainsNul, fmt.Sprintf("%s contains a NUL byte, which cannot be stored", field))
	}
	return nil
}

func lengthWithinBound(field string, length int) error {
	if length > MaxQueueLength {
		return newValidationError(codeQueueTooLong, fmt.Sprintf("%s length %d exceeds maximum %d", field, length, MaxQueueLength))
	}
	return nil
}

func (q *QueueState) CurrentTrackId() (string, bool) {
	if !indexInBounds(q.CurrentIdx, len(q.TrackIds)) {
		return "", false
	}
	return q.TrackIds[q.CurrentIdx], true
}

func indexInBounds(idx, queueLen int) bool {
	return idx >= 0 && idx < queueLen
}

func indexWithinQueue(currentIdx, queueLen int) (int, error) {
	if queueLen == 0 {
		return 0, nil
	}
	if !indexInBounds(currentIdx, queueLen) {
		return 0, newValidationError(codeCurrentIdxOutOfRange, fmt.Sprintf("currentIdx %d out of range [0, %d)", currentIdx, queueLen))
	}
	return currentIdx, nil
}

func NewQueueState(in QueueStateInput) (*QueueState, error) {
	state, err := newQueueState(in, handledNow())
	if err != nil {
		return nil, err
	}
	if err := state.currentTrackIdPresent(); err != nil {
		return nil, err
	}
	return state, nil
}

func handledNow() time.Time {
	return time.Now()
}

func RehydrateQueueState(in QueueStateInput, updatedAt time.Time) (*QueueState, error) {
	return newQueueState(in, updatedAt)
}

func EmptyQueueState(userId shared.UserId) *QueueState {
	state, err := newQueueState(QueueStateInput{UserId: userId}, handledNow())
	if err != nil {
		panic("empty queue state must always be valid: " + err.Error())
	}
	return state
}

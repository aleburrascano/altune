package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"strings"
	"time"
)

const MaxQueueLength = 10000

// MaxQueueStringBytes bounds every stored queue string: each trackIds and
// naturalOrder element and the encoded sourceId. Track identifiers are short,
// and an encoded sourceId only ever wraps a bounded search query
// (discovery.MaxSearchQueryRunes) or a playlist id/name, so 4 KiB sits far
// above any legitimate value while rejecting the ~1 MB strings that the HTTP
// body-size limit alone would otherwise let reach the domain.
const MaxQueueStringBytes = 4096

// ValidationError aliases the shared type so this package keeps one name for
// its 400s while the implementation lives in internal/shared.
type ValidationError = shared.ValidationError

// NewValidationError builds a playback validation error
// ("playback.validation_error").
func NewValidationError(msg string) *ValidationError {
	return shared.NewValidationError("playback", msg)
}

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
		return RepeatOff, NewValidationError(fmt.Sprintf("unknown repeat mode: %q", s))
	}
}

// QueueState is a user's resumable playback queue snapshot. Build it through
// NewQueueState, RehydrateQueueState or EmptyQueueState; those constructors
// reject (with a *ValidationError) any input breaking these invariants:
//   - PositionMs is >= 0.
//   - TrackIds and NaturalOrder each hold at most MaxQueueLength elements.
//   - No TrackIds or NaturalOrder element, nor SourceId, exceeds
//     MaxQueueStringBytes or contains a NUL byte.
//   - CurrentIdx is in [0, len(TrackIds)) when TrackIds is non-empty; for an
//     empty queue any input CurrentIdx is accepted and stored as 0.
//
// Constructors also normalize nil TrackIds/NaturalOrder to empty slices.
// NewQueueState and EmptyQueueState stamp UpdatedAt with the current time,
// keeping its monotonic clock reading; RehydrateQueueState keeps the stored
// one. On a save, UpdatedAt is not written as-is: the persistence adapter uses
// only its age to place the save on the database clock, which is the ordering
// the stale-write guard compares, so a stored UpdatedAt is database-clock time. The fields stay exported, so a
// struct literal or later mutation can bypass the constructors: Validate
// re-checks the same invariants and the persistence boundary calls it before
// every write. Validate does not reset CurrentIdx, so an empty queue with a
// non-zero CurrentIdx passes it unchanged.
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

// QueueStateInput is the unvalidated field set a QueueState is built from.
// NewQueueState and RehydrateQueueState return a *ValidationError unless it
// satisfies the QueueState invariants: PositionMs >= 0; TrackIds and
// NaturalOrder at most MaxQueueLength elements; every TrackIds/NaturalOrder
// element and SourceId at most MaxQueueStringBytes with no NUL byte; and
// CurrentIdx in [0, len(TrackIds)) unless TrackIds is empty, in which case it
// is ignored and the built state's CurrentIdx is 0.
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
	if err := checkQueueInvariants(in.PositionMs, trackIds, naturalOrder, in.SourceId, in.CurrentIdx); err != nil {
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

// Validate re-checks the invariants the constructors enforce. QueueState is an
// exported field bag, so a bare struct literal or a post-construction mutation
// can hold state NewQueueState would have rejected. The persistence boundary
// calls this so such a bypass can never reach a stored row.
func (q *QueueState) Validate() error {
	return checkQueueInvariants(q.PositionMs, q.TrackIds, q.NaturalOrder, q.SourceId, q.CurrentIdx)
}

func checkQueueInvariants(positionMs int64, trackIds, naturalOrder []string, sourceId string, currentIdx int) error {
	if positionMs < 0 {
		return NewValidationError(fmt.Sprintf("positionMs must be non-negative, got %d", positionMs))
	}
	if err := lengthWithinBound("trackIds", len(trackIds)); err != nil {
		return err
	}
	if err := lengthWithinBound("naturalOrder", len(naturalOrder)); err != nil {
		return err
	}
	if err := elementsStorable("trackIds", trackIds); err != nil {
		return err
	}
	if err := elementsStorable("naturalOrder", naturalOrder); err != nil {
		return err
	}
	if err := stringStorable("sourceId", sourceId); err != nil {
		return err
	}
	if _, err := indexWithinQueue(currentIdx, len(trackIds)); err != nil {
		return err
	}
	return nil
}

func emptyIfNil(trackIds []string) []string {
	if trackIds == nil {
		return []string{}
	}
	return trackIds
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
		return NewValidationError(fmt.Sprintf("%s length %d bytes exceeds maximum %d", field, len(value), MaxQueueStringBytes))
	}
	if strings.IndexByte(value, 0) >= 0 {
		return NewValidationError(fmt.Sprintf("%s contains a NUL byte, which cannot be stored", field))
	}
	return nil
}

func lengthWithinBound(field string, length int) error {
	if length > MaxQueueLength {
		return NewValidationError(fmt.Sprintf("%s length %d exceeds maximum %d", field, length, MaxQueueLength))
	}
	return nil
}

// CurrentTrackId returns the id of the track at CurrentIdx and true, or "" and
// false when no track is current. A constructed state has no current track
// only when the queue is empty. The bounds are re-checked against the live
// fields, so a state that bypassed the constructors with CurrentIdx outside
// TrackIds (including an empty queue with a non-zero CurrentIdx) also reports
// no current track rather than panicking.
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
		return 0, NewValidationError(fmt.Sprintf("currentIdx %d out of range [0, %d)", currentIdx, queueLen))
	}
	return currentIdx, nil
}

func NewQueueState(in QueueStateInput) (*QueueState, error) {
	return newQueueState(in, handledNow())
}

// handledNow stamps the instant a save is handled. It deliberately skips
// .UTC(), which would strip the monotonic clock reading: persistence measures
// the stamp's age with that reading so a wall-clock step cannot reorder saves.
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

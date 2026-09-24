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

// QueueState is a user's resumable playback queue snapshot. Build it through
// NewQueueState, RehydrateQueueState or EmptyQueueState; those constructors
// reject (with a *QueueValidationError) any input breaking these invariants:
//   - PositionMs is >= 0.
//   - TrackIds and NaturalOrder each hold at most MaxQueueLength elements.
//   - No TrackIds or NaturalOrder element, nor SourceId, exceeds
//     MaxQueueStringBytes or contains a NUL byte.
//   - CurrentIdx is in [0, len(TrackIds)) when TrackIds is non-empty; for an
//     empty queue any input CurrentIdx is accepted and stored as 0.
//   - The element at CurrentIdx is a non-empty id, on the save paths only
//     (NewQueueState and Validate). RehydrateQueueState skips this one, so a
//     row written before the rule still resumes rather than reading as
//     corrupt (#1569).
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
// NewQueueState and RehydrateQueueState return a *QueueValidationError unless it
// satisfies the QueueState invariants: PositionMs >= 0; TrackIds and
// NaturalOrder at most MaxQueueLength elements; every TrackIds/NaturalOrder
// element and SourceId at most MaxQueueStringBytes with no NUL byte; and
// CurrentIdx in [0, len(TrackIds)) unless TrackIds is empty, in which case it
// is ignored and the built state's CurrentIdx is 0. NewQueueState additionally
// requires a non-empty element at CurrentIdx; see QueueState for why
// RehydrateQueueState does not.
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

// Validate re-checks the invariants the constructors enforce. QueueState is an
// exported field bag, so a bare struct literal or a post-construction mutation
// can hold state NewQueueState would have rejected. The persistence boundary
// calls this so such a bypass can never reach a stored row.
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

// currentTrackIdPresent holds a save to what NewQueuePosition already demands
// of a position-only save: the track a queue points at is a real id. A stored
// "" there left the client unable to use PUT /queue-state/position for that
// slot, with no signal why (#1569). RehydrateQueueState deliberately skips it
// so rows written before the rule still resume instead of counting as corrupt.
func (q *QueueState) currentTrackIdPresent() error {
	if id, hasCurrent := q.CurrentTrackId(); hasCurrent && id == "" {
		return newValidationError(codeCurrentTrackIdMissing, "trackIds[currentIdx] must be a non-empty track id")
	}
	return nil
}

// queueInvariantFields is what checkQueueInvariants needs from a queue. Its
// field names mirror QueueState and QueueStateInput so both call sites map
// field-for-field, and a QueueState invariant added later arrives as a named
// field rather than a sixth positional argument.
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

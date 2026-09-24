package domain

import (
	"altune/go-api/internal/shared"
	"fmt"
	"time"
)

// QueuePosition is a position-only save: where playback is within the queue
// already stored for the user, without the track lists. It exists so the
// frequent autosave does not pay the full-queue decode, validation and write
// cost of a QueueState when only the position moved (#1126).
//
// CurrentTrackId names the track the client believes sits at CurrentIdx; the
// save applies only if the stored queue agrees, so a position can never be
// grafted onto a different queue. Build it through NewQueuePosition, which
// rejects (with a *QueueValidationError) any input breaking these invariants:
//   - PositionMs is >= 0.
//   - CurrentIdx is in [0, MaxQueueLength).
//   - CurrentTrackId is non-empty, at most MaxQueueStringBytes and has no NUL
//     byte.
//
// UpdatedAt is stamped like NewQueueState's (keeping the monotonic reading)
// and is ordered against full saves by the same database-clock stale guard.
type QueuePosition struct {
	UserId         shared.UserId
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
	UpdatedAt      time.Time
}

// QueuePositionInput is the unvalidated field set a QueuePosition is built from.
type QueuePositionInput struct {
	UserId         shared.UserId
	CurrentIdx     int
	CurrentTrackId string
	PositionMs     int64
}

// NewQueuePosition validates a position-only save and stamps it as handled now.
func NewQueuePosition(in QueuePositionInput) (*QueuePosition, error) {
	p := &QueuePosition{
		UserId:         in.UserId,
		CurrentIdx:     in.CurrentIdx,
		CurrentTrackId: in.CurrentTrackId,
		PositionMs:     in.PositionMs,
		UpdatedAt:      handledNow(),
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate re-checks NewQueuePosition's invariants; the persistence boundary
// calls it before every write, as it does QueueState.Validate.
func (p *QueuePosition) Validate() error {
	if p.PositionMs < 0 {
		return newValidationError(codePositionMsNegative, fmt.Sprintf("positionMs must be non-negative, got %d", p.PositionMs))
	}
	if !indexInBounds(p.CurrentIdx, MaxQueueLength) {
		return newValidationError(codeCurrentIdxOutOfRange, fmt.Sprintf("currentIdx %d out of range [0, %d)", p.CurrentIdx, MaxQueueLength))
	}
	if p.CurrentTrackId == "" {
		return newValidationError(codeCurrentTrackIdMissing, "currentTrackId is required")
	}
	return stringStorable("currentTrackId", p.CurrentTrackId)
}

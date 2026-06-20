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

type QueueState struct {
	UserId     shared.UserId
	TrackIds   []string
	CurrentIdx int
	PositionMs int64
	Shuffled   bool
	RepeatMode RepeatMode
	// SourceId identifies what the queue was built from (e.g. playlist ID, "library", "search").
	SourceId string
	UpdatedAt  time.Time
}

func NewQueueState(
	userId shared.UserId,
	trackIds []string,
	currentIdx int,
	positionMs int64,
	shuffled bool,
	repeatMode RepeatMode,
	sourceId string,
) (*QueueState, error) {
	if len(trackIds) == 0 {
		return &QueueState{
			UserId:     userId,
			TrackIds:   trackIds,
			CurrentIdx: 0,
			PositionMs: positionMs,
			Shuffled:   shuffled,
			RepeatMode: repeatMode,
			SourceId:   sourceId,
			UpdatedAt:  time.Now().UTC(),
		}, nil
	}
	if currentIdx < 0 || currentIdx >= len(trackIds) {
		return nil, fmt.Errorf("currentIdx %d out of range [0, %d)", currentIdx, len(trackIds))
	}
	if positionMs < 0 {
		return nil, fmt.Errorf("positionMs must be non-negative, got %d", positionMs)
	}
	return &QueueState{
		UserId:     userId,
		TrackIds:   trackIds,
		CurrentIdx: currentIdx,
		PositionMs: positionMs,
		Shuffled:   shuffled,
		RepeatMode: repeatMode,
		SourceId:   sourceId,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

package kernel

import (
	"errors"
	"math"
)

var ErrImplausibleTrackLength = errors.New("track length must be a finite number of seconds from 0 to 7 days")

const (
	maxTrackLengthSeconds      = 7 * 24 * 60 * 60
	sameRecordingSlackSeconds  = 5
	sameRecordingShareOfLength = 0.03
)

type TrackLength struct{ seconds float64 }

func TrackLengthOf(seconds float64) (TrackLength, error) {
	if math.IsNaN(seconds) || seconds < 0 || seconds > maxTrackLengthSeconds {
		return TrackLength{}, ErrImplausibleTrackLength
	}
	return TrackLength{seconds: seconds}, nil
}

func (l TrackLength) Known() bool { return l.seconds > 0 }

func (l TrackLength) allowance() float64 {
	return math.Max(sameRecordingSlackSeconds, l.seconds*sameRecordingShareOfLength)
}

func (l TrackLength) SameRecording(actual TrackLength) bool {
	if !l.Known() || !actual.Known() {
		return false
	}
	return math.Abs(l.seconds-actual.seconds) <= l.allowance()
}

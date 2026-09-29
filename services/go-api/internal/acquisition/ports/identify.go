package ports

import (
	"context"
	"errors"
)

var ErrIdentifyThrottled = errors.New("acoustid: throttled")

type LinkedRecording struct {
	MBID     string
	Title    string
	Artists  []string
	Duration float64
}

type AcoustIDResult struct {
	ID         string
	Score      float64
	Recordings []LinkedRecording
}

type RecordingMatch struct {
	AcoustID string
	MBIDs    []string
	Score    float64
	Results  []AcoustIDResult
}

func (m RecordingMatch) Known() bool { return len(m.MBIDs) > 0 || m.AcoustID != "" }

func (m RecordingMatch) Matches(mbid string) bool {
	for _, candidate := range m.MBIDs {
		if candidate == mbid {
			return true
		}
	}
	return false
}

func (m RecordingMatch) InCluster(cluster []string) bool {
	for _, id := range cluster {
		if id != "" && id == m.AcoustID {
			return true
		}
	}
	return false
}

type AudioIdentifier interface {
	Identify(ctx context.Context, filePath string, durationHint float64) (RecordingMatch, error)
	AcoustIDsFor(ctx context.Context, mbid string) ([]string, error)
}

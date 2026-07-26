package ports

import "context"

type RecordingMatch struct {
	AcoustID string
	MBIDs    []string
	Score    float64
}

func (m RecordingMatch) Known() bool { return len(m.MBIDs) > 0 }

func (m RecordingMatch) Matches(mbid string) bool {
	for _, candidate := range m.MBIDs {
		if candidate == mbid {
			return true
		}
	}
	return false
}

type AudioIdentifier interface {
	Identify(ctx context.Context, filePath string) (RecordingMatch, error)
}

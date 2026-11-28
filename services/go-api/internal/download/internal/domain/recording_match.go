package domain

type ReleaseGroup struct {
	MBID           string
	Title          string
	Type           string
	SecondaryTypes []string
}

type LinkedRecording struct {
	MBID          string
	Title         string
	Artists       []string
	Duration      float64
	ReleaseGroups []ReleaseGroup
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

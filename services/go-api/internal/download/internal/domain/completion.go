package domain

import (
	"altune/go-api/internal/shared/music"
	"encoding/json"
)

type Completion struct {
	AudioRef           string
	ReplaceAudio       bool
	Duration           music.TrackLength
	ISRC               string
	Provenance         music.AcquisitionProvenance
	RejectedSourceKeys []string
	SourceURL          string
	Confidence         float64
	Evidence           json.RawMessage
	Relabel            *TrackLabel
}

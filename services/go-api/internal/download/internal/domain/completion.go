package domain

import (
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/tracklength"
	"encoding/json"
)

type Completion struct {
	AudioRef           string
	ReplaceAudio       bool
	Duration           tracklength.TrackLength
	ISRC               string
	Provenance         downloadprovenance.AcquisitionProvenance
	RejectedSourceKeys []string
	SourceURL          string
	Confidence         float64
	Evidence           json.RawMessage
	Relabel            *TrackLabel
}

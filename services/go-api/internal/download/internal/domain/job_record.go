package domain

import "time"

type JobRecord struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          JobState
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
	Kind           JobKind
	Run            int
	StageMs        map[string]int64
	Source         SourceFormat
	Stored         StoredFile

	Confidence      float64
	SourceKey       string
	TargetSeconds   float64
	MeasuredSeconds float64
}

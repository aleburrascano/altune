package domain

type AcquisitionOutcome struct {
	TrackID         string
	Outcome         JobState
	Reason          string
	ElapsedMs       int64
	StageMs         map[string]int64
	SourceHost      string
	Source          SourceFormat
	Stored          StoredFile
	Kind            JobKind
	Run             int
	Provenance      string
	Confidence      float64
	SourceKey       string
	TargetSeconds   float64
	MeasuredSeconds float64
}

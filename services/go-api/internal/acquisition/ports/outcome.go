package ports

import "context"

type AcquisitionOutcome struct {
	TrackID   string
	Outcome   JobState
	Reason    string
	ElapsedMs int64
}

type OutcomeRecorder interface {
	Record(ctx context.Context, o AcquisitionOutcome) error
}

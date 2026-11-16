package ports

import (
	"context"
	"time"
)

type RejectionBasis struct {
	ExpectedSeconds float64
	ReferenceMBID   string
}

type CandidateRejectionRecord struct {
	TrackID   string
	SourceKey string
	Reason    string
	Detail    string
	Basis     RejectionBasis
}

type PriorRejection struct {
	SourceKey string
	Basis     *RejectionBasis
}

type RejectionStore interface {
	Record(ctx context.Context, recs []CandidateRejectionRecord) error
	Active(ctx context.Context, trackID string, since time.Time) ([]PriorRejection, error)
}

package ports

import (
	"altune/go-api/internal/shared/ids"
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
	Active(ctx context.Context, trackID ids.TrackId, since time.Time) ([]PriorRejection, error)
}

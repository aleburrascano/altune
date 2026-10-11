package ports

import (
	"context"
	"time"
)

type CandidateRejectionRecord struct {
	TrackID   string
	SourceKey string
	Reason    string
	Detail    string
}

type RejectionStore interface {
	Record(ctx context.Context, recs []CandidateRejectionRecord) error
	ActiveKeys(ctx context.Context, trackID string, since time.Time) ([]string, error)
}

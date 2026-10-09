package ports

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"context"
	"errors"
)

type FindRequest struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
	Identity RecordingIdentity
}

type AudioSource interface {
	Name() acqdomain.SourceName

	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)

	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (filePath string, err error)
}

type PreviewFetcher interface {
	FetchPreview(ctx context.Context, candidate AudioCandidate, outDir string, seconds int) (filePath string, err error)
}

func RunTimedOut(parent, run context.Context) bool {
	return parent.Err() == nil && errors.Is(run.Err(), context.DeadlineExceeded)
}

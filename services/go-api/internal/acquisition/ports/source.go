package ports

import (
	"context"
	"errors"
	"fmt"
)

type FindRequest struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
	Identity RecordingIdentity
}

type SourceName string

type AudioSource interface {
	Name() SourceName

	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)

	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (filePath string, err error)
}

type PreviewFetcher interface {
	FetchPreview(ctx context.Context, candidate AudioCandidate, outDir string, seconds int) (filePath string, err error)
}

type SourceUnavailableError struct {
	Source SourceName
	Err    error
}

func (e *SourceUnavailableError) Error() string {
	return fmt.Sprintf("source %s unavailable: %v", e.Source, e.Err)
}

func (e *SourceUnavailableError) Unwrap() error { return e.Err }

func IsSourceUnavailable(err error) bool {
	var unavailable *SourceUnavailableError
	return errors.As(err, &unavailable)
}

func RunTimedOut(parent, run context.Context) bool {
	return parent.Err() == nil && errors.Is(run.Err(), context.DeadlineExceeded)
}

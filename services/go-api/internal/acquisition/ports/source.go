package ports

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	Name() string

	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)

	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (filePath string, err error)
}

type SourceUnavailableError struct {
	Source string
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

var unavailableMarkers = []string{
	"http error 429",
	"too many requests",
	"rate limit",
	"rate-limit",
	"http error 500",
	"http error 502",
	"http error 503",
	"http error 504",
	"temporary failure in name resolution",
	"name or service not known",
	"failed to resolve",
	"connection refused",
	"connection reset",
	"connection aborted",
	"network is unreachable",
	"no route to host",
	"timed out",
}

func OutputShowsSourceUnavailable(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range unavailableMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func SearchQueries(req FindRequest) []string {
	var queries []string
	if req.ISRC != "" {
		queries = append(queries, req.ISRC)
	}
	queries = append(queries, req.Title+" "+req.Artist)
	if req.Album != "" {
		queries = append(queries, req.Title+" "+req.Artist+" "+req.Album)
	}
	queries = append(queries, req.Title+" "+req.Artist+" audio")
	return queries
}

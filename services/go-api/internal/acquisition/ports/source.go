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

// SourceUnavailableError marks a failure that is evidence about the source and
// none about the track: the provider throttled or refused us, nothing reached
// it, or its binary is not installed. Every other source failure means the
// track was searched for and not found, so without this type the pipeline tells
// a user that a track which exists does not.
type SourceUnavailableError struct {
	Source string
	Err    error
}

func (e *SourceUnavailableError) Error() string {
	return fmt.Sprintf("source %s unavailable: %v", e.Source, e.Err)
}

func (e *SourceUnavailableError) Unwrap() error { return e.Err }

// IsSourceUnavailable reports whether err, or any error it wraps, is a source
// that could not answer.
func IsSourceUnavailable(err error) bool {
	var unavailable *SourceUnavailableError
	return errors.As(err, &unavailable)
}

// unavailableMarkers are what a source CLI prints when it never got an answer
// for us. The exit status cannot carry the distinction — every source exits
// non-zero for "nothing found" and for "the provider refused us" alike — so the
// output it captured is the only signal separating the two. The table lives
// here rather than in one adapter because a throttle and a dead network read
// the same whichever CLI hit them, and both adapters must classify them alike.
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

// OutputShowsSourceUnavailable reports whether a source CLI's captured output
// names a throttle, an outage on the provider's side, or a transport failure.
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

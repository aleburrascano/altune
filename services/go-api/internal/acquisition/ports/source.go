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

	CatalogSources  []CatalogSource
	CatalogExplicit *bool

	KnownSourceURL string

	FallbackOnly bool
}

func PrefersExplicit(catalogExplicit *bool) bool {
	return catalogExplicit == nil || *catalogExplicit
}

type AudioSource interface {
	Name() acqdomain.SourceName

	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)

	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (FetchedAudio, error)
}

type FetchedAudio struct {
	Path   string
	Format SourceFormat
}

type SourceFormat struct {
	ID    string
	Codec string
	Kbps  int
}

type PreviewFetcher interface {
	FetchPreview(ctx context.Context, candidate AudioCandidate, outDir string, seconds int) (filePath string, err error)
}

func RunTimedOut(parent, run context.Context) bool {
	return parent.Err() == nil && errors.Is(run.Err(), context.DeadlineExceeded)
}

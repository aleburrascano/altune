package ports

import "context"

type PreviewTarget struct {
	ISRC          string
	DeezerTrackID string
}

type PreviewComparison struct {
	Matched        bool
	BitErrorRate   float64
	OverlapSeconds float64
	Unavailable    string
}

type PreviewMatcher interface {
	MatchesPreview(ctx context.Context, filePath string, target PreviewTarget) (PreviewComparison, error)
}

package ports

import (
	"context"
	"io"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AudioStore interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Stream(ctx context.Context, audioRef string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, audioRef string) error
}

type ReacquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId)
}

type AudioCandidate struct {
	Title          string
	Artist         string
	DurationSecs   float64
	URL            string
	Channel        string
	Categories     []string
	ViewCount      int64
	FollowerCount  int64
}

type AudioSearcher interface {
	Search(ctx context.Context, query string) ([]AudioCandidate, error)
	Download(ctx context.Context, url string, outDir string) (filePath string, err error)
}

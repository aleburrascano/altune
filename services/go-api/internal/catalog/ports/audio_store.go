package ports

import (
	"context"
	"io"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AudioStream interface {
	io.ReadSeeker
	io.Closer
}

type AudioStore interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Stream(ctx context.Context, audioRef string) (AudioStream, int64, error)
	Delete(ctx context.Context, audioRef string) error
}

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

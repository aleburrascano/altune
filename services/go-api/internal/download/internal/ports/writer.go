package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
)

type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

type AudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error)
}

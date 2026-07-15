package ports

import (
	"context"
	"io"
	"time"

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

// AudioURLSigner is an optional capability an AudioStore may implement: mint a
// short-lived URL the native player can stream directly from storage, so audio
// bytes stop being proxied through the API (which pays auth + Postgres + two
// storage round-trips per range request). Object storage (S3/OCI) satisfies it
// via presigned GET; the filesystem store does not, and callers fall back to the
// proxy stream endpoint. Detected with a type assertion, never required.
type AudioURLSigner interface {
	PresignGet(ctx context.Context, audioRef string, ttl time.Duration) (string, error)
}

type AcquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

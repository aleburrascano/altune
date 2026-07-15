// Package ports defines the interfaces the acquisition use cases consume.
// They are owned here, by the consumer, and speak acquisition's own
// AudioCandidate plus the catalog Track aggregate (acquisition is a customer of
// the catalog supplier; Track and its MarkReady/MarkFailed invariants stay in
// catalog/domain).
package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

// AudioCandidate is one downloadable result surfaced by an AudioSearcher.
type AudioCandidate struct {
	Title         string
	Artist        string
	DurationSecs  float64
	URL           string
	Channel       string
	Categories    []string
	ViewCount     int64
	FollowerCount int64
}

// AudioSearcher finds and downloads audio for a query. Implemented by the
// yt-dlp adapter.
type AudioSearcher interface {
	Search(ctx context.Context, query string) ([]AudioCandidate, error)
	Download(ctx context.Context, url string, outDir string) (filePath string, err error)
}

// AudioProber inspects a downloaded audio file before it is stored. ProbeDuration
// reads its true length (to reject wrong-recording matches). ValidateDecodable
// decodes the stream to confirm the samples are actually playable: a container can
// carry valid metadata (duration, codec) over corrupt audio data that no player
// can decode, which ProbeDuration alone (metadata-only) does not catch. Implemented
// by the ffprobe/ffmpeg adapter.
type AudioProber interface {
	ProbeDuration(ctx context.Context, filePath string) (float64, error)
	ValidateDecodable(ctx context.Context, filePath string) error
}

// AudioWriter is the write-side of audio storage acquisition needs: persist a
// downloaded file, check existence, and roll back. The catalog AudioStore
// adapter satisfies it (its read-side Stream is not part of this seam).
type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

// TrackRepository is the narrowed track read/update surface acquisition uses to
// reconcile a track's acquisition status. The catalog repository satisfies it.
type TrackRepository interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
}

package ports

import (
	"context"
	"io"
	"strings"
	"time"
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

// AudioContentType maps a stored audio ref to its MIME type — the single source
// for both the upload side (object storage sets it on PutObject) and the serve
// side (the proxy stream endpoint labels the response). The two must agree:
// iOS/expo-audio decodes progressive audio by Content-Type, so an m4a sent as
// audio/mpeg fails to play. Defaults to audio/mpeg for legacy mp3 refs.
func AudioContentType(audioRef string) string {
	switch {
	case strings.HasSuffix(audioRef, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(audioRef, ".opus"):
		return "audio/opus"
	case strings.HasSuffix(audioRef, ".ogg"):
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
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

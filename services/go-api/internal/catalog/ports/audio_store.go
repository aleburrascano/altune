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

// MaxPresignTTL is the hard ceiling on a presigned audio URL's lifetime. Every
// AudioURLSigner clamps the requested ttl to it before signing, so no call site
// (a new option, or a bug) can mint a long-lived bearer link to a user's audio.
const MaxPresignTTL = time.Hour

// ClampPresignTTL caps ttl at MaxPresignTTL. A non-positive ttl is returned
// unchanged so the signer can reject it rather than silently widening it.
func ClampPresignTTL(ttl time.Duration) time.Duration {
	return min(ttl, MaxPresignTTL)
}

type AudioURLSigner interface {
	PresignGet(ctx context.Context, audioRef string, ttl time.Duration) (string, error)
}

type AudioLister interface {
	List(ctx context.Context, prefix string) ([]string, error)
}

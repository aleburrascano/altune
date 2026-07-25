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

type AudioURLSigner interface {
	PresignGet(ctx context.Context, audioRef string, ttl time.Duration) (string, error)
}

type AudioLister interface {
	List(ctx context.Context, prefix string) ([]string, error)
}

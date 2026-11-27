package audiofiles

import (
	"altune/go-api/internal/library/internal/core/ports"
	"context"
	"fmt"
	"log/slog"
	"strings"
)

var _ ports.AudioStore = (*scopedAudioStore)(nil)

type scopedAudioStore struct {
	inner  ports.AudioStore
	prefix string
}

func (s *scopedAudioStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	return s.inner.Exists(ctx, audioRef)
}

func (s *scopedAudioStore) Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error) {
	return s.inner.Stream(ctx, audioRef)
}

func (s *scopedAudioStore) Store(ctx context.Context, sourcePath, audioRef string) error {
	if !strings.HasPrefix(audioRef, s.prefix) {
		return fmt.Errorf("scoped audio store: refusing to write %q outside prefix %q", audioRef, s.prefix)
	}
	return s.inner.Store(ctx, sourcePath, audioRef)
}

func (s *scopedAudioStore) Delete(ctx context.Context, audioRef string) error {
	if !strings.HasPrefix(audioRef, s.prefix) {
		slog.WarnContext(ctx, "scoped_audio_store.delete_skipped_outside_prefix",
			"audio_ref", audioRef, "prefix", s.prefix)
		return nil
	}
	return s.inner.Delete(ctx, audioRef)
}

type scopedAgeLister struct {
	inner  ports.AudioAgeLister
	prefix string
}

func (l scopedAgeLister) ListWithAge(ctx context.Context, prefix string) ([]ports.ObjectAge, error) {
	if !strings.HasPrefix(prefix, l.prefix) {
		return nil, fmt.Errorf("scoped audio store: refusing to list %q outside prefix %q", prefix, l.prefix)
	}
	return l.inner.ListWithAge(ctx, prefix)
}

func NewScopedAudioBackend(backend ports.AudioBackend, prefix string) ports.AudioBackend {
	return ports.AudioBackend{
		AudioStore: &scopedAudioStore{inner: backend.AudioStore, prefix: prefix},
		Links:      backend.Links,
		Ages:       scopedAgeLister{inner: backend.Ages, prefix: prefix},
	}
}

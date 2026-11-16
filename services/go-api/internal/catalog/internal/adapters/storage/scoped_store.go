package storage

import (
	"altune/go-api/internal/catalog/ports"
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

type withSigner struct {
	*scopedAudioStore
	ports.AudioURLSigner
}

type withLister struct {
	*scopedAudioStore
	ports.AudioLister
}

type withAgeLister struct {
	*scopedAudioStore
	ports.AudioAgeLister
}

type withSignerLister struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
}

type withSignerAgeLister struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioAgeLister
}

type withListerAgeLister struct {
	*scopedAudioStore
	ports.AudioLister
	ports.AudioAgeLister
}

type withSignerListerAgeLister struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
	ports.AudioAgeLister
}

func NewScopedAudioStore(store ports.AudioStore, prefix string) ports.AudioStore {
	base := &scopedAudioStore{inner: store, prefix: prefix}
	signer, hasSigner := store.(ports.AudioURLSigner)
	lister, hasLister := store.(ports.AudioLister)
	innerAgeLister, hasAgeLister := store.(ports.AudioAgeLister)
	var ageLister ports.AudioAgeLister
	if hasAgeLister {
		ageLister = scopedAgeLister{inner: innerAgeLister, prefix: prefix}
	}

	switch {
	case hasSigner && hasLister && hasAgeLister:
		return &withSignerListerAgeLister{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister, AudioAgeLister: ageLister}
	case hasSigner && hasLister:
		return &withSignerLister{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister}
	case hasSigner && hasAgeLister:
		return &withSignerAgeLister{scopedAudioStore: base, AudioURLSigner: signer, AudioAgeLister: ageLister}
	case hasLister && hasAgeLister:
		return &withListerAgeLister{scopedAudioStore: base, AudioLister: lister, AudioAgeLister: ageLister}
	case hasSigner:
		return &withSigner{scopedAudioStore: base, AudioURLSigner: signer}
	case hasLister:
		return &withLister{scopedAudioStore: base, AudioLister: lister}
	case hasAgeLister:
		return &withAgeLister{scopedAudioStore: base, AudioAgeLister: ageLister}
	default:
		return base
	}
}

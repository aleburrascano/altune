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

type scopedAudioStoreSigner struct {
	*scopedAudioStore
	ports.AudioURLSigner
}

type scopedAudioStoreLister struct {
	*scopedAudioStore
	ports.AudioLister
}

type scopedAudioStoreSignerLister struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
}

func NewScopedAudioStore(store ports.AudioStore, prefix string) ports.AudioStore {
	base := &scopedAudioStore{inner: store, prefix: prefix}
	signer, hasSigner := store.(ports.AudioURLSigner)
	lister, hasLister := store.(ports.AudioLister)
	switch {
	case hasSigner && hasLister:
		return &scopedAudioStoreSignerLister{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister}
	case hasSigner:
		return &scopedAudioStoreSigner{scopedAudioStore: base, AudioURLSigner: signer}
	case hasLister:
		return &scopedAudioStoreLister{scopedAudioStore: base, AudioLister: lister}
	default:
		return base
	}
}

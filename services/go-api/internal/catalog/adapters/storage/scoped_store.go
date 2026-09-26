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

type scopedCopier struct {
	inner  ports.AudioCopier
	prefix string
}

func (c scopedCopier) Copy(ctx context.Context, srcRef, dstRef string) error {
	if !strings.HasPrefix(srcRef, c.prefix) || !strings.HasPrefix(dstRef, c.prefix) {
		return fmt.Errorf("scoped audio store: refusing to copy %q -> %q outside prefix %q", srcRef, dstRef, c.prefix)
	}
	return c.inner.Copy(ctx, srcRef, dstRef)
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

type withCopier struct {
	*scopedAudioStore
	ports.AudioCopier
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

type withSignerCopier struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioCopier
}

type withListerAgeLister struct {
	*scopedAudioStore
	ports.AudioLister
	ports.AudioAgeLister
}

type withListerCopier struct {
	*scopedAudioStore
	ports.AudioLister
	ports.AudioCopier
}

type withAgeListerCopier struct {
	*scopedAudioStore
	ports.AudioAgeLister
	ports.AudioCopier
}

type withSignerListerAgeLister struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
	ports.AudioAgeLister
}

type withSignerListerCopier struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
	ports.AudioCopier
}

type withSignerAgeListerCopier struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioAgeLister
	ports.AudioCopier
}

type withListerAgeListerCopier struct {
	*scopedAudioStore
	ports.AudioLister
	ports.AudioAgeLister
	ports.AudioCopier
}

type withSignerListerAgeListerCopier struct {
	*scopedAudioStore
	ports.AudioURLSigner
	ports.AudioLister
	ports.AudioAgeLister
	ports.AudioCopier
}

func NewScopedAudioStore(store ports.AudioStore, prefix string) ports.AudioStore {
	base := &scopedAudioStore{inner: store, prefix: prefix}
	signer, hasSigner := store.(ports.AudioURLSigner)
	lister, hasLister := store.(ports.AudioLister)
	innerAgeLister, hasAgeLister := store.(ports.AudioAgeLister)
	innerCopier, hasCopier := store.(ports.AudioCopier)
	var ageLister ports.AudioAgeLister
	if hasAgeLister {
		ageLister = scopedAgeLister{inner: innerAgeLister, prefix: prefix}
	}
	var copier ports.AudioCopier
	if hasCopier {
		copier = scopedCopier{inner: innerCopier, prefix: prefix}
	}

	switch {
	case hasSigner && hasLister && hasAgeLister && hasCopier:
		return &withSignerListerAgeListerCopier{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister, AudioAgeLister: ageLister, AudioCopier: copier}
	case hasSigner && hasLister && hasAgeLister:
		return &withSignerListerAgeLister{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister, AudioAgeLister: ageLister}
	case hasSigner && hasLister && hasCopier:
		return &withSignerListerCopier{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister, AudioCopier: copier}
	case hasSigner && hasAgeLister && hasCopier:
		return &withSignerAgeListerCopier{scopedAudioStore: base, AudioURLSigner: signer, AudioAgeLister: ageLister, AudioCopier: copier}
	case hasLister && hasAgeLister && hasCopier:
		return &withListerAgeListerCopier{scopedAudioStore: base, AudioLister: lister, AudioAgeLister: ageLister, AudioCopier: copier}
	case hasSigner && hasLister:
		return &withSignerLister{scopedAudioStore: base, AudioURLSigner: signer, AudioLister: lister}
	case hasSigner && hasAgeLister:
		return &withSignerAgeLister{scopedAudioStore: base, AudioURLSigner: signer, AudioAgeLister: ageLister}
	case hasSigner && hasCopier:
		return &withSignerCopier{scopedAudioStore: base, AudioURLSigner: signer, AudioCopier: copier}
	case hasLister && hasAgeLister:
		return &withListerAgeLister{scopedAudioStore: base, AudioLister: lister, AudioAgeLister: ageLister}
	case hasLister && hasCopier:
		return &withListerCopier{scopedAudioStore: base, AudioLister: lister, AudioCopier: copier}
	case hasAgeLister && hasCopier:
		return &withAgeListerCopier{scopedAudioStore: base, AudioAgeLister: ageLister, AudioCopier: copier}
	case hasSigner:
		return &withSigner{scopedAudioStore: base, AudioURLSigner: signer}
	case hasLister:
		return &withLister{scopedAudioStore: base, AudioLister: lister}
	case hasAgeLister:
		return &withAgeLister{scopedAudioStore: base, AudioAgeLister: ageLister}
	case hasCopier:
		return &withCopier{scopedAudioStore: base, AudioCopier: copier}
	default:
		return base
	}
}

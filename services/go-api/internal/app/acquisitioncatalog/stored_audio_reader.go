package acquisitioncatalog

import (
	"altune/go-api/internal/catalog"
	"context"
	"io"
	"os"
	"path/filepath"
)

type StoredAudioReader struct {
	store catalog.AudioStore
}

func NewStoredAudioReader(store catalog.AudioStore) StoredAudioReader {
	return StoredAudioReader{store: store}
}

func (r StoredAudioReader) CopyStored(ctx context.Context, audioRef, dir string) (string, error) {
	stream, _, err := r.store.Stream(ctx, audioRef)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()

	path := filepath.Join(dir, "stored"+filepath.Ext(audioRef))
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, stream); err != nil {
		_ = dst.Close()
		return "", err
	}
	return path, dst.Close()
}

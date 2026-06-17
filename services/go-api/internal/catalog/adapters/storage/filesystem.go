package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"altune/go-api/internal/catalog/ports"
)

var _ ports.AudioStore = (*FilesystemAudioStore)(nil)

type FilesystemAudioStore struct {
	baseDir string
}

func NewFilesystemAudioStore(baseDir string) *FilesystemAudioStore {
	return &FilesystemAudioStore{baseDir: baseDir}
}

func (s *FilesystemAudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *FilesystemAudioStore) Store(_ context.Context, sourcePath string, audioRef string) error {
	destPath, err := s.safePath(audioRef)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return os.Rename(sourcePath, destPath)
}

func (s *FilesystemAudioStore) Stream(_ context.Context, audioRef string) (ports.AudioStream, int64, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return nil, 0, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}

	return file, stat.Size(), nil
}

func (s *FilesystemAudioStore) Delete(_ context.Context, audioRef string) error {
	path, err := s.safePath(audioRef)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *FilesystemAudioStore) safePath(audioRef string) (string, error) {
	if !filepath.IsLocal(audioRef) {
		return "", fmt.Errorf("path traversal rejected: %s", audioRef)
	}
	return filepath.Join(s.baseDir, audioRef), nil
}

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

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

	if err := os.Rename(sourcePath, destPath); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("move audio into place: %w", err)
	}

	// Source (temp dir) and destination (audio volume) are on different
	// filesystems, so os.Rename returns EXDEV. Copy across, then remove the
	// source. Without this, every acquisition fails when $TMPDIR and the audio
	// baseDir are separate mounts (the norm on a Linux VM).
	if err := copyFile(sourcePath, destPath); err != nil {
		return fmt.Errorf("copy audio into place: %w", err)
	}
	if err := os.Remove(sourcePath); err != nil {
		slog.Warn("audio_temp_source_remove_failed", "path", sourcePath, "error", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// Stream returns a readable file handle and its size in bytes.
// The caller MUST close the returned AudioStream when done.
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

package memory

import (
	"altune/go-api/internal/library/internal/core/ports"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
)

type AudioStore struct {
	Files map[string][]byte

	ErrOnExists error
	ErrOnStore  error
	ErrOnStream error
	ErrOnDelete error
}

var _ ports.AudioStore = (*AudioStore)(nil)

func NewAudioStore() *AudioStore {
	return &AudioStore{Files: make(map[string][]byte)}
}

func (s *AudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	if s.ErrOnExists != nil {
		return false, s.ErrOnExists
	}
	_, ok := s.Files[audioRef]
	return ok, nil
}

func (s *AudioStore) Store(_ context.Context, sourcePath, audioRef string) error {
	if s.ErrOnStore != nil {
		return s.ErrOnStore
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}
	s.Files[audioRef] = content
	return nil
}

func (s *AudioStore) Stream(_ context.Context, audioRef string) (ports.AudioStream, int64, error) {
	if s.ErrOnStream != nil {
		return nil, 0, s.ErrOnStream
	}
	content, ok := s.Files[audioRef]
	if !ok {
		return nil, 0, &fs.PathError{Op: "open", Path: audioRef, Err: fs.ErrNotExist}
	}
	return audioStream{bytes.NewReader(content)}, int64(len(content)), nil
}

type audioStream struct{ *bytes.Reader }

func (audioStream) Close() error { return nil }

func (s *AudioStore) Delete(_ context.Context, audioRef string) error {
	if s.ErrOnDelete != nil {
		return s.ErrOnDelete
	}
	delete(s.Files, audioRef)
	return nil
}

func (s *AudioStore) Seed(audioRef string, content []byte) {
	s.Files[audioRef] = content
}

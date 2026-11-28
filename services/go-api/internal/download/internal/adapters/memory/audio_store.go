package memory

import (
	"altune/go-api/internal/download/internal/ports"
	"context"
	"fmt"
	"os"
	"sync"
)

type AudioStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

var _ ports.AudioWriter = (*AudioStore)(nil)

func NewAudioStore() *AudioStore { return &AudioStore{files: map[string][]byte{}} }

func (s *AudioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.files[audioRef]
	return ok, nil
}

func (s *AudioStore) Store(_ context.Context, sourcePath, audioRef string) error {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read the file to store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[audioRef] = content
	return nil
}

func (s *AudioStore) Delete(_ context.Context, audioRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, audioRef)
	return nil
}

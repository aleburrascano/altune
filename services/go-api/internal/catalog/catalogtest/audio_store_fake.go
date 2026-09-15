package catalogtest

import (
	"altune/go-api/internal/catalog/ports"
	"bytes"
	"context"
	"io"
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

func (s *AudioStore) Store(_ context.Context, _, audioRef string) error {
	if s.ErrOnStore != nil {
		return s.ErrOnStore
	}
	s.Files[audioRef] = []byte("audio-data")
	return nil
}

func (s *AudioStore) Stream(_ context.Context, audioRef string) (ports.AudioStream, int64, error) {
	if s.ErrOnStream != nil {
		return nil, 0, s.ErrOnStream
	}
	data, ok := s.Files[audioRef]
	if !ok {
		return nil, 0, io.EOF
	}
	return audioStream{bytes.NewReader(data)}, int64(len(data)), nil
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

func (s *AudioStore) Seed(audioRef string, data []byte) {
	s.Files[audioRef] = data
}

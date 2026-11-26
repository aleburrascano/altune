package filefloor

import (
	"altune/go-api/internal/shared/events/internal/core/idfloor"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type Store struct {
	path string
}

var _ idfloor.Store = (*Store)(nil)

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Location() string { return s.path }

func (s *Store) Load() ([]byte, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, idfloor.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Store) Save(raw []byte) error {
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace %s: %w", s.path, err)
	}
	return nil
}

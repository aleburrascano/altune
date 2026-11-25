package fsprobe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func Readable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file for reading: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close readable probe: %w", err)
	}
	return nil
}

func Stat(path string) (fs.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}
	return info, nil
}

func TempFile(dir, pattern string) (cleanup func() error, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("create write probe: %w", err)
	}
	name := f.Name()
	return func() error {
		closeErr := f.Close()
		return errors.Join(closeErr, os.Remove(name))
	}, nil
}

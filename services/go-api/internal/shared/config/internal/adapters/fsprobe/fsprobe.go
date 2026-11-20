package fsprobe

import (
	"errors"
	"io/fs"
	"os"
)

func Readable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func TempFile(dir, pattern string) (cleanup func() error, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	name := f.Name()
	return func() error {
		closeErr := f.Close()
		return errors.Join(closeErr, os.Remove(name))
	}, nil
}

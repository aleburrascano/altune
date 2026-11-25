package fsprobe

import (
	"fmt"
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

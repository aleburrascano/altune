package fsprobe_test

import (
	"altune/go-api/internal/shared/config/internal/adapters/fsprobe"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadable_AcceptsAFileItCanOpenAndNamesWhyItCannot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := fsprobe.Readable(file); err != nil {
		t.Fatalf("Readable(existing file) = %v, want nil", err)
	}
	err := fsprobe.Readable(filepath.Join(dir, "missing.txt"))
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "open file for reading") {
		t.Fatalf("Readable(missing file) = %v, want a not-exist error saying it could not open the file", err)
	}
}

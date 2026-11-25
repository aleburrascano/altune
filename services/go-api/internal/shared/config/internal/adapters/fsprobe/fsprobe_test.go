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

func TestStat_ReturnsTheInfoOfAPathAndNamesWhyItCannot(t *testing.T) {
	dir := t.TempDir()

	info, err := fsprobe.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Stat(dir) = %v, %v, want a directory and nil", info, err)
	}
	_, err = fsprobe.Stat(filepath.Join(dir, "missing"))
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "stat path") {
		t.Fatalf("Stat(missing) = %v, want a not-exist error saying it could not stat the path", err)
	}
}

func TestTempFile_CreatesAProbeThatCleanupRemoves(t *testing.T) {
	dir := t.TempDir()

	cleanup, err := fsprobe.TempFile(dir, ".probe-*")
	if err != nil {
		t.Fatalf("TempFile = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %d entries after TempFile, want the one probe", len(entries))
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup = %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("directory holds %d entries after cleanup, want none", len(entries))
	}
	if _, err := fsprobe.TempFile(filepath.Join(dir, "missing"), ".probe-*"); err == nil || !strings.Contains(err.Error(), "create write probe") {
		t.Fatalf("TempFile(missing dir) = %v, want an error saying it could not create the probe", err)
	}
}

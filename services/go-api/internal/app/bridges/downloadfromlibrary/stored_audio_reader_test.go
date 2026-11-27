package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/library"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStoredAudioReader_CopiesTheStoredFileKeepingItsExtension(t *testing.T) {
	store := library.NewFilesystemAudioStore(t.TempDir(), library.NoopFSCallMetrics())
	source := filepath.Join(t.TempDir(), "in.m4a")
	if err := os.WriteFile(source, []byte("stored audio bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(t.Context(), source, "audio/a.m4a"); err != nil {
		t.Fatalf("store: %v", err)
	}
	dir := t.TempDir()

	path, err := downloadfromlibrary.NewStoredAudioReader(store).CopyStored(t.Context(), "audio/a.m4a", dir)
	if err != nil {
		t.Fatalf("CopyStored: %v", err)
	}

	copied, err := os.Open(path)
	if err != nil {
		t.Fatalf("open copy: %v", err)
	}
	defer func() { _ = copied.Close() }()
	got, err := io.ReadAll(copied)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(got) != "stored audio bytes" || filepath.Dir(path) != dir || filepath.Ext(path) != ".m4a" {
		t.Errorf("copy = %q at %s, want the stored bytes inside %s keeping the .m4a extension", got, path, dir)
	}
}

func TestStoredAudioReader_ReportsAMissingStoredFile(t *testing.T) {
	store := library.NewFilesystemAudioStore(t.TempDir(), library.NoopFSCallMetrics())

	if _, err := downloadfromlibrary.NewStoredAudioReader(store).CopyStored(t.Context(), "audio/missing.m4a", t.TempDir()); err == nil {
		t.Fatal("CopyStored of a ref that is not stored: want error, got nil")
	}
}

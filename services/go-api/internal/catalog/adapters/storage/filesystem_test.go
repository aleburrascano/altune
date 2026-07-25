package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilesystemAudioStore_StoreAndStream(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	content := []byte("fake audio data for store-and-stream test")
	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	if err := store.Store(ctx, srcPath, "tracks/abc.opus"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	rc, size, err := store.Stream(ctx, "tracks/abc.opus")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("streamed content: got %q, want %q", got, content)
	}
	if size != int64(len(content)) {
		t.Errorf("streamed size: got %d, want %d", size, len(content))
	}
}

func TestFilesystemAudioStore_Exists(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(ctx, srcPath, "exists-test.opus"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	exists, err := store.Exists(ctx, "exists-test.opus")
	if err != nil {
		t.Fatalf("Exists (stored): %v", err)
	}
	if !exists {
		t.Error("expected stored file to exist, got false")
	}

	exists, err = store.Exists(ctx, "no-such-file.opus")
	if err != nil {
		t.Fatalf("Exists (missing): %v", err)
	}
	if exists {
		t.Error("expected non-existent file to return false, got true")
	}
}

func TestFilesystemAudioStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, []byte("to-delete"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(ctx, srcPath, "delete-me.opus"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := store.Delete(ctx, "delete-me.opus"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err := store.Exists(ctx, "delete-me.opus")
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("expected file to not exist after delete, got true")
	}
}

func TestFilesystemAudioStore_Stream_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	rc, _, err := store.Stream(ctx, "nonexistent.opus")
	if err == nil {
		rc.Close()
		t.Fatal("expected error streaming non-existent file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestFilesystemAudioStore_SafePath(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	traversalRefs := []string{
		"../etc/passwd",
		"tracks/../../secret",
	}
	if runtime.GOOS == "windows" {
		traversalRefs = append(traversalRefs, "..\\windows\\system32")
	}

	assertRejected := func(t *testing.T, ref string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("expected path traversal to be rejected for %q, got nil error", ref)
			return
		}
		if !strings.Contains(err.Error(), "path traversal rejected") {
			t.Errorf("expected 'path traversal rejected' in error, got: %v", err)
		}
	}

	for _, ref := range traversalRefs {
		t.Run("Exists_"+ref, func(t *testing.T) {
			_, err := store.Exists(ctx, ref)
			assertRejected(t, ref, err)
		})
		t.Run("Stream_"+ref, func(t *testing.T) {
			_, _, err := store.Stream(ctx, ref)
			assertRejected(t, ref, err)
		})
		t.Run("Delete_"+ref, func(t *testing.T) {
			assertRejected(t, ref, store.Delete(ctx, ref))
		})
		t.Run("Store_"+ref, func(t *testing.T) {
			assertRejected(t, ref, store.Store(ctx, "/tmp/whatever", ref))
		})
	}
}

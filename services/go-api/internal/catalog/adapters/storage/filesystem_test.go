package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestFilesystemDelete_MissingFileIsNoError(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	ctx := context.Background()

	t.Run("never stored", func(t *testing.T) {
		if err := store.Delete(ctx, "never-stored.opus"); err != nil {
			t.Errorf("Delete of a ref that was never stored: got %v, want nil", err)
		}
	})

	t.Run("already deleted", func(t *testing.T) {
		srcPath := filepath.Join(dir, "source-twice.opus")
		if err := os.WriteFile(srcPath, []byte("to-delete-twice"), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if err := store.Store(ctx, srcPath, "delete-twice.opus"); err != nil {
			t.Fatalf("Store: %v", err)
		}
		if err := store.Delete(ctx, "delete-twice.opus"); err != nil {
			t.Fatalf("first Delete: %v", err)
		}

		if err := store.Delete(ctx, "delete-twice.opus"); err != nil {
			t.Errorf("second Delete: got %v, want nil", err)
		}
	})
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

func TestBoundedFSCall_ReturnsCallResultUnchanged(t *testing.T) {
	sentinel := errors.New("boom")
	got, err := boundedFSCall(context.Background(), "op", func() (int, error) {
		return 7, sentinel
	}, nil)
	if got != 7 || !errors.Is(err, sentinel) || err.Error() != sentinel.Error() {
		t.Fatalf("got (%d, %v), want (7, the call's own error)", got, err)
	}
}

func TestBoundedFSCall_AlreadyDoneContextSkipsCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := boundedFSCall(ctx, "op", func() (struct{}, error) {
		called = true
		return struct{}{}, nil
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if called {
		t.Error("call ran despite an already-cancelled context")
	}
}

func TestBoundedFSCall_ExpiryDiscardsLateResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	release := make(chan struct{})
	discarded := make(chan int, 1)
	_, err := boundedFSCall(ctx, "op", func() (int, error) {
		<-release
		return 42, nil
	}, func(late int) { discarded <- late })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	close(release)
	select {
	case got := <-discarded:
		if got != 42 {
			t.Errorf("discarded %d, want the late result 42", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late result was never discarded: its resource would leak")
	}
}

func TestFilesystemAudioStore_CancelledContextDoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemAudioStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "kept.opus"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Exists(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Exists: expected context.Canceled, got %v", err)
	}
	if _, _, err := store.Stream(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Stream: expected context.Canceled, got %v", err)
	}
	if err := store.Delete(ctx, "kept.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: expected context.Canceled, got %v", err)
	}
	src := filepath.Join(dir, "src.opus")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := store.Store(ctx, src, "stored.opus"); !errors.Is(err, context.Canceled) {
		t.Errorf("Store: expected context.Canceled, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept.opus")); err != nil {
		t.Errorf("Delete with a cancelled context removed the file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stored.opus")); !os.IsNotExist(err) {
		t.Errorf("Store with a cancelled context moved the file: %v", err)
	}
}

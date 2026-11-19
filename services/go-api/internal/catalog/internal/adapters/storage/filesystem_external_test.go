package storage_test

import (
	"altune/go-api/internal/catalog"
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFilesystemAudioStore_StoreAndStream(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
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
	store := catalog.NewFilesystemAudioStore(dir)
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
	store := catalog.NewFilesystemAudioStore(dir)
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
	store := catalog.NewFilesystemAudioStore(dir)
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
	store := catalog.NewFilesystemAudioStore(dir)
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
	store := catalog.NewFilesystemAudioStore(dir)
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

func TestFilesystemAudioStore_ReturnsTheFilesystemsOwnError(t *testing.T) {
	store := catalog.NewFilesystemAudioStore(t.TempDir())

	_, _, err := store.Stream(context.Background(), "missing.opus")
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || err.Error() != pathErr.Error() {
		t.Fatalf("Stream = %v, want the filesystem's own *fs.PathError, unwrapped", err)
	}
}

func TestFilesystemAudioStore_Stream_CancelledContextNeverOpensTheFile(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
	path := makeFifo(t, dir, "untouched.opus")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := store.Stream(ctx, "untouched.opus"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if readerAppears(path, 200*time.Millisecond) {
		t.Fatal("Stream opened the file despite an already-cancelled context")
	}
}

func TestFilesystemAudioStore_Stream_ClosesAFileThatOpensAfterTheDeadline(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
	path := makeFifo(t, dir, "late.opus")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := streamWithHangGuard(t, ctx, store, "late.opus"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if !lateReaderClosed(t, path) {
		t.Fatal("the file that opened after the deadline was never closed, so it leaks")
	}
}

func TestFilesystemAudioStore_ListWithAge(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
	ctx := context.Background()

	for _, ref := range []string{"staging/u1/a/song1.opus", "staging/u1/a/song2.opus", "u2/other.opus"} {
		src := filepath.Join(t.TempDir(), "src.opus")
		if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if err := store.Store(ctx, src, ref); err != nil {
			t.Fatalf("Store %q: %v", ref, err)
		}
	}

	objects, err := store.ListWithAge(ctx, "staging/")
	if err != nil {
		t.Fatalf("ListWithAge: %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("got %d objects, want 2: %+v", len(objects), objects)
	}
	seen := map[string]bool{}
	for _, obj := range objects {
		seen[obj.AudioRef] = true
		if obj.LastModified.IsZero() {
			t.Errorf("%q: LastModified is zero", obj.AudioRef)
		}
		if obj.LastModified.After(time.Now()) {
			t.Errorf("%q: LastModified %s is in the future", obj.AudioRef, obj.LastModified)
		}
	}
	if !seen["staging/u1/a/song1.opus"] || !seen["staging/u1/a/song2.opus"] {
		t.Errorf("expected both staging refs listed, got %+v", objects)
	}
}

func TestFilesystemAudioStore_ListWithAge_MissingPrefixIsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)

	objects, err := store.ListWithAge(context.Background(), "staging/")
	if err != nil {
		t.Fatalf("ListWithAge on missing prefix: %v", err)
	}
	if len(objects) != 0 {
		t.Errorf("got %d objects, want 0: %+v", len(objects), objects)
	}
}

func TestFilesystemAudioStore_CancelledContextDoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
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

const stallHangGuard = 5 * time.Second

type streamResult struct {
	err error
}

func streamWithHangGuard(t *testing.T, ctx context.Context, store catalog.AudioStore, ref string) error {
	t.Helper()
	done := make(chan streamResult, 1)
	go func() {
		rc, _, err := store.Stream(ctx, ref)
		if err == nil {
			rc.Close()
		}
		done <- streamResult{err: err}
	}()
	select {
	case res := <-done:
		return res.err
	case <-time.After(stallHangGuard):
		t.Fatalf("Stream on a stalled file still blocked after %s: context is ignored", stallHangGuard)
		return nil
	}
}

func TestFilesystemAudioStore_Stream_StalledOpenHonoursCallerDeadline(t *testing.T) {
	dir := t.TempDir()
	store := catalog.NewFilesystemAudioStore(dir)
	makeStalledAudio(t, dir, "stalled.opus")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := streamWithHangGuard(t, ctx, store, "stalled.opus")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stream returned after %s, want roughly the 100ms caller deadline", elapsed)
	}
}

type contextRecordingHandler struct {
	slog.Handler
	records chan contextRecord
}

type contextRecord struct {
	message string
	ctx     context.Context
}

func (h contextRecordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h contextRecordingHandler) Handle(ctx context.Context, r slog.Record) error {
	select {
	case h.records <- contextRecord{message: r.Message, ctx: ctx}:
	default:
	}
	return nil
}

type recordedContextKey struct{}

func recordSlog(t *testing.T) chan contextRecord {
	t.Helper()
	records := make(chan contextRecord, 4)
	previous := slog.Default()
	previousOutput, previousFlags := log.Writer(), log.Flags()
	slog.SetDefault(slog.New(contextRecordingHandler{records: records}))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	return records
}

func TestFilesystemAudioStore_StoreAcrossFilesystems_RemoveFailureStillSucceedsAndLogsWithContext(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "src")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "source.opus")
	if err := os.WriteFile(sourcePath, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	store := catalog.NewFilesystemAudioStore(otherFilesystemDir(t, sourceDir))
	if err := os.Chmod(sourceDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sourceDir, 0o755) })

	records := recordSlog(t)
	ctx := context.WithValue(context.Background(), recordedContextKey{}, "marker")

	if err := store.Store(ctx, sourcePath, "dest.opus"); err != nil {
		t.Fatalf("Store across filesystems = %v, want nil", err)
	}
	assertRemoveFailureLoggedWithContext(t, records)
}

func assertRemoveFailureLoggedWithContext(t *testing.T, records chan contextRecord) {
	t.Helper()
	select {
	case record := <-records:
		if record.message != "audio_temp_source_remove_failed" {
			t.Fatalf("logged %q, want audio_temp_source_remove_failed", record.message)
		}
		if record.ctx.Value(recordedContextKey{}) != "marker" {
			t.Fatal("log record did not carry the caller's context")
		}
	default:
		t.Fatal("no remove-failure log recorded")
	}
}

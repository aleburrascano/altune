package storage

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var _ ports.AudioStore = (*FilesystemAudioStore)(nil)

// FilesystemAudioStore keeps audio under baseDir, which may be a network-backed
// mount (NFS, SMB, FUSE). A stalled mount blocks stat/open/rename/unlink
// indefinitely and no Go syscall can be cancelled, so every method runs its
// filesystem work through boundedFSCall: the caller-visible wait ends at the
// context deadline (or opTimeout) even though the syscall itself does not (#1064).
type FilesystemAudioStore struct {
	baseDir string
	// opTimeout caps the metadata operations (Exists, Delete, and Stream's
	// open+stat), mirroring storageOpTimeout in the object-storage adapter so a
	// caller without its own deadline still cannot hang on a wedged mount. Store
	// is bounded by the caller's context only: a cross-filesystem copy is
	// proportional to file size, like the object-storage upload.
	opTimeout time.Duration
}

func NewFilesystemAudioStore(baseDir string) *FilesystemAudioStore {
	return &FilesystemAudioStore{baseDir: baseDir, opTimeout: storageOpTimeout}
}

func (s *FilesystemAudioStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	return boundedFSCall(ctx, "exists", func() (bool, error) {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil, err
	}, nil)
}

func (s *FilesystemAudioStore) Store(ctx context.Context, sourcePath, audioRef string) error {
	destPath, err := s.safePath(audioRef)
	if err != nil {
		return err
	}

	_, err = boundedFSCall(ctx, "store", func() (struct{}, error) {
		return struct{}{}, moveIntoPlace(sourcePath, destPath)
	}, nil)
	return err
}

func moveIntoPlace(sourcePath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	err := os.Rename(sourcePath, destPath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("move audio into place: %w", err)
	}
	return copyThenRemoveAcrossFilesystems(sourcePath, destPath)
}

func copyThenRemoveAcrossFilesystems(sourcePath, destPath string) error {
	if err := copyFile(sourcePath, destPath); err != nil {
		return fmt.Errorf("copy audio into place: %w", err)
	}
	if err := os.Remove(sourcePath); err != nil {
		slog.Warn("audio_temp_source_remove_failed", "path", sourcePath, "error", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// openedAudio is the result of Stream's bounded open+stat.
type openedAudio struct {
	file *os.File
	size int64
}

// Stream bounds only the open+stat. Reads on the returned file are not bounded
// here: they are driven by the HTTP handler, whose request context and write
// deadlines (#1277) own the long-lived playback budget.
func (s *FilesystemAudioStore) Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	opened, err := boundedFSCall(ctx, "stream open", func() (openedAudio, error) {
		return openAudio(path)
	}, closeLateAudio)
	if err != nil {
		return nil, 0, err
	}
	return opened.file, opened.size, nil
}

func openAudio(path string) (openedAudio, error) {
	file, err := os.Open(path)
	if err != nil {
		return openedAudio{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return openedAudio{}, err
	}
	return openedAudio{file: file, size: stat.Size()}, nil
}

// closeLateAudio releases a descriptor whose open finished after the caller
// gave up; nobody else holds it, so leaving it open would leak it.
func closeLateAudio(late openedAudio) {
	if late.file == nil {
		return
	}
	if err := late.file.Close(); err != nil {
		slog.Warn("audio_late_open_close_failed", "path", late.file.Name(), "error", err)
	}
}

// Delete is idempotent: an audio file that is already gone is the outcome the
// caller asked for, so it succeeds rather than reporting a failed delete
// (#2201). Object storage's RemoveObject behaves the same way, which keeps the
// two AudioStore implementations interchangeable for a retried or concurrent
// delete, and for a file removed outside the app.
func (s *FilesystemAudioStore) Delete(ctx context.Context, audioRef string) error {
	path, err := s.safePath(audioRef)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	_, err = boundedFSCall(ctx, "delete", func() (struct{}, error) {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return struct{}{}, nil
		}
		return struct{}{}, err
	}, nil)
	return err
}

func (s *FilesystemAudioStore) safePath(audioRef string) (string, error) {
	if !filepath.IsLocal(audioRef) {
		return "", fmt.Errorf("path traversal rejected: %s", audioRef)
	}
	return filepath.Join(s.baseDir, audioRef), nil
}

type fsCallResult[T any] struct {
	value T
	err   error
}

// boundedFSCall runs call on its own goroutine and returns when it finishes or
// ctx is done, whichever is first. A ctx that is already done fails fast without
// touching the filesystem. On ctx expiry the error wraps ctx.Err(), so
// errors.Is(err, context.DeadlineExceeded) holds; otherwise call's own result
// and error are returned unchanged (os.IsNotExist still works on them).
//
// Trade-off: a blocking syscall cannot be interrupted in Go, so an abandoned
// call's goroutine stays parked in the kernel until the mount recovers, or
// forever if it never does. That is one parked goroutine per timed-out call
// (bounded by request concurrency) in exchange for never wedging the caller.
// The result channel is buffered so that goroutine exits without a receiver
// once the syscall returns, and discard (if non-nil) releases any resource a
// late successful result owns. Side effects still land after the caller saw a
// timeout: an abandoned Store may finish moving the file into place (the
// orphaned-audio reconcile job, #1297, sweeps it) and an abandoned Delete may
// still remove it.
func boundedFSCall[T any](ctx context.Context, op string, call func() (T, error), discard func(T)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("filesystem %s: %w", op, err)
	}

	done := make(chan fsCallResult[T], 1)
	go func() {
		value, err := call()
		done <- fsCallResult[T]{value: value, err: err}
	}()

	select {
	case res := <-done:
		return res.value, res.err
	case <-ctx.Done():
		if discard != nil {
			go discardLateResult(done, discard)
		}
		return zero, fmt.Errorf("filesystem %s: %w", op, ctx.Err())
	}
}

func discardLateResult[T any](done <-chan fsCallResult[T], discard func(T)) {
	res := <-done
	if res.err == nil {
		discard(res.value)
	}
}

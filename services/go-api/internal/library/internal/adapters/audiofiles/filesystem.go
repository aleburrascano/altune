package audiofiles

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/library/internal/core/ports"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	_ ports.AudioStore     = (*FilesystemAudioStore)(nil)
	_ ports.AudioAgeLister = (*FilesystemAudioStore)(nil)
)

type FilesystemAudioStore struct {
	baseDir   string
	opTimeout time.Duration
	gate      *fsGate
}

func NewFilesystemAudioStore(baseDir string) *FilesystemAudioStore {
	return newFilesystemAudioStore(baseDir, storageOpTimeout)
}

func newFilesystemAudioStore(baseDir string, opTimeout time.Duration) *FilesystemAudioStore {
	if opTimeout <= 0 {
		opTimeout = storageOpTimeout
	}
	return &FilesystemAudioStore{baseDir: baseDir, opTimeout: opTimeout, gate: &fsGate{}}
}

func (s *FilesystemAudioStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	return boundedFSCall(ctx, s.gate, "exists", func() (bool, error) {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("stat audio %s: %w", path, err)
		}
		return true, nil
	}, nil)
}

func (s *FilesystemAudioStore) Store(ctx context.Context, sourcePath, audioRef string) error {
	destPath, err := s.safePath(audioRef)
	if err != nil {
		return err
	}

	_, err = boundedFSCall(ctx, s.gate, "store", func() (struct{}, error) {
		return struct{}{}, moveIntoPlace(ctx, sourcePath, destPath)
	}, nil)
	return err
}

func moveIntoPlace(ctx context.Context, sourcePath, destPath string) error {
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
	return copyThenRemoveAcrossFilesystems(ctx, sourcePath, destPath)
}

func copyThenRemoveAcrossFilesystems(ctx context.Context, sourcePath, destPath string) error {
	if err := copyFile(sourcePath, destPath); err != nil {
		return fmt.Errorf("copy audio into place: %w", err)
	}
	if err := os.Remove(sourcePath); err != nil {
		slog.WarnContext(ctx, "audio_temp_source_remove_failed", "path", sourcePath, "error", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

type openedAudio struct {
	file   *os.File
	size   int64
	opened bool
}

func (s *FilesystemAudioStore) Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error) {
	path, err := s.safePath(audioRef)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	opened, err := boundedFSCall(ctx, s.gate, "stream open", func() (openedAudio, error) {
		return openAudio(path)
	}, func(late openedAudio) { closeLateAudio(ctx, late) })
	if err != nil {
		return nil, 0, err
	}
	return opened.file, opened.size, nil
}

func openAudio(path string) (openedAudio, error) {
	file, err := os.Open(path)
	if err != nil {
		return openedAudio{}, fmt.Errorf("open audio: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return openedAudio{}, fmt.Errorf("stat audio %s: %w", path, err)
	}
	return openedAudio{file: file, size: stat.Size(), opened: true}, nil
}

func closeLateAudio(ctx context.Context, late openedAudio) {
	if !late.opened {
		return
	}
	if err := late.file.Close(); err != nil {
		slog.WarnContext(ctx, "audio_late_open_close_failed", "path", late.file.Name(), "error", err)
	}
}

func (s *FilesystemAudioStore) Delete(ctx context.Context, audioRef string) error {
	path, err := s.safePath(audioRef)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	_, err = boundedFSCall(ctx, s.gate, "delete", func() (struct{}, error) {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return struct{}{}, nil
		}
		if err != nil {
			return struct{}{}, fmt.Errorf("remove audio %s: %w", path, err)
		}
		return struct{}{}, nil
	}, nil)
	return err
}

func (s *FilesystemAudioStore) ListWithAge(ctx context.Context, prefix string) ([]ports.ObjectAge, error) {
	root, err := s.safePath(prefix)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.opTimeout)
	defer cancel()

	objects, err := boundedFSCall(ctx, s.gate, "list", func() ([]ports.ObjectAge, error) {
		return walkObjectAges(ctx, s.baseDir, root)
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("list %q: %w", prefix, err)
	}
	return objects, nil
}

func walkObjectAges(ctx context.Context, baseDir, root string) ([]ports.ObjectAge, error) {
	var objects []ports.ObjectAge
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		obj, ok, err := objectAgeFor(ctx, baseDir, path, entry, walkErr)
		if err != nil || !ok {
			return err
		}
		objects = append(objects, obj)
		return nil
	})
	if err != nil {
		return objects, fmt.Errorf("walk audio objects under %s: %w", root, err)
	}
	return objects, nil
}

func objectAgeFor(ctx context.Context, baseDir, path string, entry os.DirEntry, walkErr error) (ports.ObjectAge, bool, error) {
	if walkErr != nil {
		return ports.ObjectAge{}, false, walkErrOrNilIfMissing(walkErr)
	}
	if ctx.Err() != nil {
		return ports.ObjectAge{}, false, fmt.Errorf("list audio objects: %w", ctx.Err())
	}
	if entry.IsDir() {
		return ports.ObjectAge{}, false, nil
	}
	info, err := entry.Info()
	if err != nil {
		return ports.ObjectAge{}, false, fmt.Errorf("read modification time of %s: %w", path, err)
	}
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return ports.ObjectAge{}, false, fmt.Errorf("relate %s to the audio directory: %w", path, err)
	}
	return ports.ObjectAge{AudioRef: filepath.ToSlash(rel), LastModified: info.ModTime()}, true, nil
}

func walkErrOrNilIfMissing(err error) error {
	if os.IsNotExist(err) {
		return nil
	}
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

const MaxInFlightFilesystemCalls = 64

type fsGate struct {
	inFlight atomic.Int64
}

func (g *fsGate) enter() bool {
	if g.inFlight.Add(1) > MaxInFlightFilesystemCalls {
		g.inFlight.Add(-1)
		return false
	}
	return true
}

func (g *fsGate) leave() { g.inFlight.Add(-1) }

func boundedFSCall[T any](ctx context.Context, gate *fsGate, op string, call func() (T, error), discard func(T)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("filesystem %s: %w", op, err)
	}
	if !gate.enter() {
		slog.WarnContext(ctx, "catalog.storage_stalled", "op", op, "in_flight", MaxInFlightFilesystemCalls)
		return zero, fmt.Errorf("filesystem %s: %w", op, domain.ErrStorageStalled)
	}

	done := make(chan fsCallResult[T], 1)
	go func() {
		defer gate.leave()
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

package scratchfiles

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ ports.ScratchFolder = (*Workspace)(nil)

type Workspace struct{}

func New() *Workspace { return &Workspace{} }

func (*Workspace) Create(pattern string) (string, error) {
	dir, err := os.MkdirTemp("", domain.TempDirPrefix+pattern)
	if err != nil {
		return "", fmt.Errorf("create a temp workspace: %w", err)
	}
	return dir, nil
}

func (*Workspace) Remove(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove a temp workspace: %w", err)
	}
	return nil
}

func (*Workspace) Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("size a temp workspace: %w", err)
	}
	return info.Size(), nil
}

func (*Workspace) SweepAbandoned(ctx context.Context, abandonedBefore time.Time) int {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.temp_sweep_unreadable", "dir", root, logging.KeyError, redact.LogError(err))
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if isAbandoned(entry, abandonedBefore) && removeStale(ctx, filepath.Join(root, entry.Name())) {
			removed++
		}
	}
	if removed > 0 {
		slog.InfoContext(ctx, "acquisition.stale_temp_dirs_swept", "dir", root, "removed", removed)
	}
	return removed
}

func removeStale(ctx context.Context, path string) bool {
	if err := os.RemoveAll(path); err != nil {
		slog.WarnContext(ctx, "acquisition.stale_temp_removal_failed", logging.KeyPath, path, logging.KeyError, redact.LogError(err))
		return false
	}
	return true
}

func isAbandoned(entry os.DirEntry, abandonedBefore time.Time) bool {
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), domain.TempDirPrefix) {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.ModTime().Before(abandonedBefore)
}

package tempfs

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ ports.TempWorkspace = (*Workspace)(nil)

type Workspace struct{}

func New() *Workspace { return &Workspace{} }

func (*Workspace) Create(pattern string) (string, error) {
	return os.MkdirTemp("", ports.TempDirPrefix+pattern)
}

func (*Workspace) Remove(path string) error {
	return os.RemoveAll(path)
}

func (*Workspace) Size(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (*Workspace) SweepAbandoned(abandonedBefore time.Time) int {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.WarnContext(context.Background(), "acquisition.temp_sweep_unreadable", "dir", root, "error", redact.LogError(err))
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if isAbandoned(entry, abandonedBefore) && removeStale(filepath.Join(root, entry.Name())) {
			removed++
		}
	}
	if removed > 0 {
		slog.InfoContext(context.Background(), "acquisition.stale_temp_dirs_swept", "dir", root, "removed", removed)
	}
	return removed
}

func removeStale(path string) bool {
	if err := os.RemoveAll(path); err != nil {
		slog.WarnContext(context.Background(), "acquisition.stale_temp_removal_failed", "path", path, "error", redact.LogError(err))
		return false
	}
	return true
}

func isAbandoned(entry os.DirEntry, abandonedBefore time.Time) bool {
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ports.TempDirPrefix) {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.ModTime().Before(abandonedBefore)
}

package tempfs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/redact"
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

func (*Workspace) SweepAbandoned(abandonedBefore time.Time) int {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.Warn("acquisition.temp_sweep_unreadable", "dir", root, "error", redact.LogError(err))
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if !isAbandoned(entry, abandonedBefore) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("acquisition.stale_temp_removal_failed", "path", path, "error", redact.LogError(err))
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("acquisition.stale_temp_dirs_swept", "dir", root, "removed", removed)
	}
	return removed
}

func isAbandoned(entry os.DirEntry, abandonedBefore time.Time) bool {
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ports.TempDirPrefix) {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.ModTime().Before(abandonedBefore)
}

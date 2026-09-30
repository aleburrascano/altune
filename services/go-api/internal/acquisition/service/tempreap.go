package service

import (
	"altune/go-api/internal/acquisition/ports"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const tempDirPrefix = ports.TempDirPrefix

const maxLiveTempAge = acquireTimeout + 15*time.Minute

func SweepStaleTempDirs() {
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		slog.Warn("acquisition.temp_sweep_unreadable", "dir", root, "error", logSafeError(err))
		return
	}
	abandoned := abandonedTempDirs(entries, time.Now().Add(-maxLiveTempAge))
	if removed := removeTempDirs(root, abandoned); removed > 0 {
		slog.Info("acquisition.stale_temp_dirs_swept", "dir", root, "removed", removed)
	}
}

func abandonedTempDirs(entries []os.DirEntry, abandonedBefore time.Time) []string {
	var abandoned []string
	for _, entry := range entries {
		if isAbandonedTempDir(entry, abandonedBefore) {
			abandoned = append(abandoned, entry.Name())
		}
	}
	return abandoned
}

func isAbandonedTempDir(entry os.DirEntry, abandonedBefore time.Time) bool {
	if !entry.IsDir() || !strings.HasPrefix(entry.Name(), tempDirPrefix) {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.ModTime().Before(abandonedBefore)
}

func removeTempDirs(root string, names []string) int {
	removed := 0
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("acquisition.stale_temp_removal_failed", "path", path, "error", logSafeError(err))
			continue
		}
		removed++
	}
	return removed
}

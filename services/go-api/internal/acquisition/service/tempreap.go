package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tempDirPrefix is the literal prefix of step_download's MkdirTemp pattern. The
// creation site and the sweep must name the same prefix or a leak is
// unreachable, so nothing else may match it.
const tempDirPrefix = "altune-acquire-"

// maxLiveTempAge is the age past which no running acquisition can still own an
// altune-acquire-* dir: the job's own acquireTimeout bounds how long its dir can
// be written to, and the margin absorbs a coarse or skewed filesystem mtime.
// Reaping late costs one more startup's worth of disk; reaping early destroys a
// download that is still running, here or in a sibling process sharing the dir.
const maxLiveTempAge = acquireTimeout + 15*time.Minute

// SweepStaleTempDirs removes the altune-acquire-* dirs in os.TempDir() that no
// running acquisition can still own. step_download's defers cover error, cancel
// and panic, but not SIGKILL, an OOM kill, or a redeploy that outlasts the drain
// deadline, and each leak can hold a full audio download. Call once at startup,
// before the scheduler accepts jobs.
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

// isAbandonedTempDir holds the whole claim that a path may be deleted: it is one
// of ours by name, a real directory rather than a file or a symlink to
// elsewhere, and last written before any live job could have touched it. An
// entry whose info cannot be read is kept, since unreadable is not evidence of
// abandonment.
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

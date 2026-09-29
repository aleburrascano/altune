package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

func CleanupTemp(ctx context.Context, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	root := ac.tempRoot()
	if err := os.RemoveAll(root); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", root, "error", err)
	}
}

func (ac *AcquisitionContext) tempRoot() string {
	if ac.TempDir != "" {
		return ac.TempDir
	}
	return filepath.Dir(ac.TempPath)
}

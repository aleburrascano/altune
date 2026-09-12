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
	parent := filepath.Dir(ac.TempPath)
	if err := os.RemoveAll(parent); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", parent, "error", err)
	}
}

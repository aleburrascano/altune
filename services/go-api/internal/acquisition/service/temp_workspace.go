package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
	"path/filepath"
	"time"
)

var defaultTempWorkspace ports.TempWorkspace

const maxLiveTempAge = AcquireTimeout + 15*time.Minute

func SweepStaleTempDirs(ws ports.TempWorkspace) {
	ws.SweepAbandoned(time.Now().Add(-maxLiveTempAge))
}

func CleanupTemp(ctx context.Context, ws ports.TempWorkspace, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	root := ac.tempRoot()
	if err := ws.Remove(root); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", root, "error", err)
	}
}

func (ac *AcquisitionContext) tempRoot() string {
	if ac.TempDir != "" {
		return ac.TempDir
	}
	return filepath.Dir(ac.TempPath)
}

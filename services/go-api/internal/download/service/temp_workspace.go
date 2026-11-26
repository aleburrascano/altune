package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"errors"
	"log/slog"
	"time"
)

var ErrNoTempWorkspace = errors.New("no temp workspace configured")

const maxLiveTempAge = AcquireTimeout + 15*time.Minute

func SweepStaleTempDirs(ws ports.ScratchFolder) {
	ws.SweepAbandoned(time.Now().Add(-maxLiveTempAge))
}

func CleanupTemp(ctx context.Context, ws ports.ScratchFolder, ac *AcquisitionContext) {
	if ws == nil || ac.TempPath == "" {
		return
	}
	path := ac.TempDir
	if path == "" {
		path = ac.TempPath
	}
	if err := ws.Remove(path); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", path, "error", err)
	}
}

package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"time"
)

const maxLiveTempAge = AcquireTimeout + 15*time.Minute

func requireWorkspace(owner string, ws ports.ScratchFolder) ports.ScratchFolder {
	if ws == nil {
		panic(owner + ": a temp workspace is required")
	}
	return ws
}

func SweepStaleTempDirs(ws ports.ScratchFolder) {
	ws.SweepAbandoned(time.Now().Add(-maxLiveTempAge))
}

func CleanupTemp(ctx context.Context, ws ports.ScratchFolder, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	path := ac.TempDir
	if path == "" {
		path = ac.TempPath
	}
	removeScratch(ctx, ws, path)
}

func removeScratch(ctx context.Context, ws ports.ScratchFolder, path string) {
	if err := ws.Remove(path); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", path, "error", redact.LogError(err))
	}
}

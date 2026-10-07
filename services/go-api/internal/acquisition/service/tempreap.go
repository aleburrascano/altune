package service

import (
	"altune/go-api/internal/acquisition/ports"
	"time"
)

const tempDirPrefix = ports.TempDirPrefix

var defaultTempWorkspace ports.TempWorkspace

const maxLiveTempAge = acquireTimeout + 15*time.Minute

func SweepStaleTempDirs(ws ports.TempWorkspace) {
	ws.SweepAbandoned(time.Now().Add(-maxLiveTempAge))
}

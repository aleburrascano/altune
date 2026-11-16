package app

import (
	"altune/overseer/internal/buckets/heartbeat"
	"altune/overseer/internal/core"
	"time"

	_ "altune/overseer/internal/buckets/backendperf"
	_ "altune/overseer/internal/buckets/cost"
	_ "altune/overseer/internal/buckets/domainquality"
	_ "altune/overseer/internal/buckets/liveactivity"
	_ "altune/overseer/internal/buckets/logs"
	_ "altune/overseer/internal/buckets/reliability"
	_ "altune/overseer/internal/buckets/security"
	_ "altune/overseer/internal/buckets/usage"
)

func Buckets() []core.Bucket {
	built := []core.Bucket{
		heartbeat.New(heartbeat.Deps{Now: time.Now}),
	}
	return append(built, core.Default.Buckets()...)
}

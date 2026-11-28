package downloadtest

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
)

type DownloadQueue = ports.DownloadQueue

func NewPostgresJobQueue(db ports.Database, requeuer ports.QueueTracks) DownloadQueue {
	return db.JobQueue(requeuer)(nil, clock.Now)
}

func MustNewPostgresInPlaceRecorder(db ports.Database, verdicts ports.InPlaceVerdicts) ports.InPlaceStore {
	recorder, err := service.NewInPlaceRecorder(verdicts, db.Upgrades())
	if err != nil {
		panic(err)
	}
	return recorder
}

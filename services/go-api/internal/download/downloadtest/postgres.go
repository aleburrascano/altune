package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
)

type PostgresJobQueue = persistence.PostgresJobQueue

func NewPostgresJobQueue(db *persistence.Database, requeuer ports.QueueTracks) *PostgresJobQueue {
	return db.JobQueue(requeuer)
}

func MustNewPostgresInPlaceRecorder(db *persistence.Database, verdicts ports.InPlaceVerdicts) ports.InPlaceStore {
	recorder, err := service.NewInPlaceRecorder(verdicts, db.UpgradeStore())
	if err != nil {
		panic(err)
	}
	return recorder
}

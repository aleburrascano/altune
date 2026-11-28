package persistence

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct{ pool *pgxpool.Pool }

var _ ports.Database = (*Database)(nil)

func NewDatabase(pool *pgxpool.Pool) ports.Database {
	if pool == nil {
		return nil
	}
	return &Database{pool: pool}
}

func (db *Database) Rejections() ports.RejectionStore {
	return NewPostgresRejectionStore(db.pool)
}

func (db *Database) Outcomes() ports.OutcomeRecorder {
	return NewPostgresOutcomeStore(db.pool)
}

func (db *Database) Upgrades() ports.UpgradeRecords {
	return NewPostgresUpgradeStore(db.pool)
}

func (db *Database) Cooldowns() ports.CooldownStore {
	return NewPostgresCooldownStore(db.pool)
}

func (db *Database) JobQueue(requeuer ports.QueueTracks) ports.QueueFactory {
	queue := MustNewPostgresJobQueue(db.pool, requeuer)
	return func(chan<- struct{}, clock.Func) ports.DownloadQueue { return queue }
}

func (db *Database) JobNotifier(sleep clock.Sleeper) ports.JobNotifier {
	return MustNewPostgresJobNotifier(db.pool, sleep)
}

func (db *Database) Retention() (outcomes, rejections ports.RetentionPruner) {
	return NewPostgresOutcomeStore(db.pool), NewPostgresRejectionStore(db.pool)
}

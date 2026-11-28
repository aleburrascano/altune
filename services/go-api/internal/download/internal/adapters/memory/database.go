package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"context"
	"time"
)

type Database struct {
	rejections *RejectionRepo
	upgrades   *UpgradeState
	cooldowns  *CooldownStore
}

var _ ports.Database = (*Database)(nil)

func NewDatabase(now clock.Func) *Database {
	return &Database{
		rejections: NewRejectionRepo(now),
		upgrades:   NewUpgradeState(now),
		cooldowns:  MustNewCooldownStore(now),
	}
}

func (db *Database) Rejections() ports.RejectionStore { return db.rejections }

func (db *Database) Outcomes() ports.OutcomeRecorder { return forgetsOutcomes{} }

func (db *Database) Upgrades() ports.UpgradeRecords { return db.upgrades }

func (db *Database) Cooldowns() ports.CooldownStore { return db.cooldowns }

func (db *Database) JobQueue(ports.QueueTracks) ports.QueueFactory { return NewQueueFactory() }

func (db *Database) JobNotifier(clock.Sleeper) ports.JobNotifier { return neverWakes{} }

func (db *Database) Retention() (outcomes, rejections ports.RetentionPruner) {
	return keepsEverything{}, keepsEverything{}
}

type forgetsOutcomes struct{}

func (forgetsOutcomes) Record(context.Context, domain.AcquisitionOutcome) error { return nil }

type neverWakes struct{}

func (neverWakes) Listen(context.Context, chan<- struct{}) {}

type keepsEverything struct{}

func (keepsEverything) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

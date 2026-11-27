package persistence

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct{ pool *pgxpool.Pool }

func NewDatabase(pool *pgxpool.Pool) *Database {
	if pool == nil {
		return nil
	}
	return &Database{pool: pool}
}

func (db *Database) pgxPool() *pgxpool.Pool {
	if db == nil {
		return nil
	}
	return db.pool
}

func (db *Database) Rejections() *PostgresRejectionStore {
	return NewPostgresRejectionStore(db.pgxPool())
}

func (db *Database) Outcomes() *PostgresOutcomeStore {
	return NewPostgresOutcomeStore(db.pgxPool())
}

func (db *Database) UpgradeStore() *PostgresUpgradeStore {
	return NewPostgresUpgradeStore(db.pgxPool())
}

func (db *Database) JobQueue(requeuer ports.TrackRequeuer) *PostgresJobQueue {
	return MustNewPostgresJobQueue(db.pgxPool(), requeuer)
}

func (db *Database) JobNotifier(sleep clock.Sleeper) *PostgresJobNotifier {
	return MustNewPostgresJobNotifier(db.pgxPool(), sleep)
}

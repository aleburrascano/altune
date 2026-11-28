package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type contractPgxJobQueue struct {
	*persistence.PostgresJobQueue
	pool *pgxpool.Pool
}

func (q contractPgxJobQueue) AddTrack(t *testing.T, userID ids.UserId) ids.TrackId {
	t.Helper()
	return insertFailedTrackForUser(t, q.pool, userID)
}

func (q contractPgxJobQueue) ExpireLease(t *testing.T, trackID ids.TrackId) {
	expireLease(t, q.pool, trackID)
}

func TestPostgresJobQueue_Contract(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	acquisitiontest.JobQueueContract(t, func(*testing.T) acquisitiontest.JobQueueUnderTest {
		return contractPgxJobQueue{PostgresJobQueue: persistence.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool}), pool: pool}
	})
}

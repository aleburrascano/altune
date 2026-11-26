package persistence_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
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
	testdb.RequireIntegration(t)
	pool := newPool(t)
	acquisitiontest.JobQueueContract(t, func(*testing.T) acquisitiontest.JobQueueUnderTest {
		return contractPgxJobQueue{PostgresJobQueue: download.NewPostgresJobQueue(pool), pool: pool}
	})
}

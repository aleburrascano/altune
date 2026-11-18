package persistence_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/sharedtest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type contractPgxJobQueue struct {
	*acquisition.PgxJobQueue
	pool *pgxpool.Pool
}

func (q contractPgxJobQueue) AddTrack(t *testing.T, userID shared.UserId) shared.TrackId {
	t.Helper()
	return insertFailedTrackForUser(t, q.pool, userID)
}

func (q contractPgxJobQueue) ExpireLease(t *testing.T, trackID shared.TrackId) {
	expireLease(t, q.pool, trackID)
}

func TestPgxJobQueue_Contract(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	acquisitiontest.JobQueueContract(t, func(*testing.T) acquisitiontest.JobQueueUnderTest {
		return contractPgxJobQueue{PgxJobQueue: acquisition.NewPgxJobQueue(pool), pool: pool}
	})
}

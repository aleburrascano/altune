package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type trackedUpgradeState struct {
	*persistence.PostgresUpgradeStore
	pool *pgxpool.Pool
}

func (s trackedUpgradeState) AddTrack(t *testing.T) ids.TrackId {
	t.Helper()
	return insertFailedTrackForUser(t, s.pool, ids.NewUserId(uuid.New()))
}

func (s trackedUpgradeState) SeedEvidence(t *testing.T, trackID ids.TrackId, evidence json.RawMessage) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO acquisition_upgrade_state AS s (track_id, evidence) VALUES ($1, $2)
		 ON CONFLICT (track_id) DO UPDATE SET evidence = EXCLUDED.evidence`,
		trackID.UUID(), []byte(evidence))
	if err != nil {
		t.Fatalf("seed upgrade evidence: %v", err)
	}
}

func TestPostgresUpgradeStore_KeepsTheUpgradeStateRepoContract(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	contracttest.UpgradeStateRepoContract(t, func(*testing.T) contracttest.UpgradeStateRepoUnderTest {
		return trackedUpgradeState{PostgresUpgradeStore: persistence.NewPostgresUpgradeStore(pool), pool: pool}
	})
}

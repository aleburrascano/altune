package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/shared/dbtest"
	"testing"
)

func TestPostgresRejectionStore_KeepsTheRejectionRepoContract(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	contracttest.RejectionRepoContract(t, func(*testing.T) contracttest.RejectionRepoUnderTest {
		return persistence.NewPostgresRejectionStore(pool)
	})
}

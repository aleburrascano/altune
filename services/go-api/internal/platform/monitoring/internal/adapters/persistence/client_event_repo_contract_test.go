package persistence_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/persistence"
	"altune/go-api/internal/platform/monitoring/internal/contracttest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresClientEvents struct {
	*persistence.PostgresClientEventStore
	pool *pgxpool.Pool
}

func (postgresClientEvents) NewUser(*testing.T) ids.UserId { return newOwner() }

func (p postgresClientEvents) RegisterIdentity(t *testing.T, user ids.UserId) {
	t.Helper()
	if _, err := p.pool.Exec(t.Context(), `INSERT INTO auth.users (id) VALUES ($1)`, user.UUID()); err != nil {
		t.Fatalf("register the surviving identity: %v", err)
	}
}

func (p postgresClientEvents) Count(t *testing.T, user ids.UserId) int {
	t.Helper()
	return countClientEvents(t, p.pool, user)
}

func TestPostgresClientEventRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.ClientEventRepoContract(t, func(t *testing.T) contracttest.ClientEventRepoUnderTest {
		pool := dbtest.Pool(t)
		standUpIdentityStore(t, pool)
		return postgresClientEvents{PostgresClientEventStore: persistence.NewPostgresClientEventStore(pool), pool: pool}
	})
}

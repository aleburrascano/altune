package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func contractUser(t *testing.T, pool *pgxpool.Pool, tables ...string) ids.UserId {
	t.Helper()
	user := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		for _, table := range tables {
			_, _ = pool.Exec(context.WithoutCancel(t.Context()), `DELETE FROM `+table+` WHERE user_id = $1`, user.UUID())
		}
	})
	return user
}

type favoritesUnderContract struct {
	*persistence.PostgresFavoritesRepository
	pool *pgxpool.Pool
}

func (f favoritesUnderContract) NewUser(t *testing.T) ids.UserId {
	return contractUser(t, f.pool, "discovery_favorites")
}

func (f favoritesUnderContract) SeedFavorites(t *testing.T, user ids.UserId, count int) {
	t.Helper()
	seedFavorites(t, f.pool, user, count)
}

func TestPostgresFavoritesRepository_KeepsTheRepoContract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.FavoritesRepoContract(t, func(*testing.T) contracttest.FavoritesRepoUnderTest {
		return favoritesUnderContract{persistence.NewPostgresFavoritesRepository(pool), pool}
	})
}

type historyUnderContract struct {
	*persistence.PostgresSearchHistoryRepository
	pool *pgxpool.Pool
}

func (h historyUnderContract) NewUser(t *testing.T) ids.UserId {
	return contractUser(t, h.pool, "discovery_search_history", "discovery_search_history_cleared")
}

func TestPostgresSearchHistoryRepository_KeepsTheRepoContract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.HistoryRepoContract(t, func(*testing.T) contracttest.HistoryRepoUnderTest {
		return historyUnderContract{persistence.NewPostgresSearchHistoryRepository(pool), pool}
	})
}

type eventsUnderContract struct {
	*persistence.PostgresEventStore
	pool *pgxpool.Pool
}

func (e eventsUnderContract) NewUser(t *testing.T) ids.UserId {
	return contractUser(t, e.pool, "discovery_events")
}

func TestPostgresEventStore_KeepsTheRepoContract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.EventRepoContract(t, func(*testing.T) contracttest.EventRepoUnderTest {
		return eventsUnderContract{persistence.NewPostgresEventStore(pool, clock.Now), pool}
	})
}

type identityUnderContract struct {
	*persistence.PostgresIdentityStore
	pool *pgxpool.Pool
}

func (i identityUnderContract) SeedIdentity(t *testing.T, kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string) {
	t.Helper()
	seedIdentity(t, i.pool, kind, mbid, xref)
	t.Cleanup(func() {
		for provider, externalID := range xref {
			_, _ = i.pool.Exec(context.WithoutCancel(t.Context()),
				`DELETE FROM entity_identity WHERE provider = $1 AND external_id = $2`, provider, externalID)
		}
	})
}

func TestPostgresIdentityStore_KeepsTheRepoContract(t *testing.T) {
	pool := dbtest.Pool(t)
	contracttest.IdentityRepoContract(t, func(*testing.T) contracttest.IdentityRepoUnderTest {
		return identityUnderContract{persistence.NewPostgresIdentityStore(pool), pool}
	})
}

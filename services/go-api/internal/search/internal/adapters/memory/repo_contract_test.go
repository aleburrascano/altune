package memory_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func newUser(*testing.T) ids.UserId { return ids.NewUserId(uuid.New()) }

type favoritesUnderContract struct{ *memory.FavoritesRepo }

func (favoritesUnderContract) NewUser(t *testing.T) ids.UserId { return newUser(t) }

func (f favoritesUnderContract) SeedFavorites(t *testing.T, user ids.UserId, count int) {
	t.Helper()
	for i := range count {
		key := "seed|" + strconv.Itoa(i)
		if err := f.Add(t.Context(), user, domain.Favorite{Kind: domain.ResultKindAlbum, Key: key, Title: key}); err != nil {
			t.Fatalf("seed favorite %d: %v", i, err)
		}
	}
}

func TestFavoritesRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.FavoritesRepoContract(t, func(*testing.T) contracttest.FavoritesRepoUnderTest {
		return favoritesUnderContract{&memory.FavoritesRepo{}}
	})
}

type historyUnderContract struct{ *memory.HistoryRepo }

func (historyUnderContract) NewUser(t *testing.T) ids.UserId { return newUser(t) }

func TestHistoryRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.HistoryRepoContract(t, func(*testing.T) contracttest.HistoryRepoUnderTest {
		return historyUnderContract{&memory.HistoryRepo{}}
	})
}

type eventsUnderContract struct{ *memory.EventRepo }

func (eventsUnderContract) NewUser(t *testing.T) ids.UserId { return newUser(t) }

func TestEventRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.EventRepoContract(t, func(*testing.T) contracttest.EventRepoUnderTest {
		return eventsUnderContract{&memory.EventRepo{}}
	})
}

type identityUnderContract struct{ *memory.IdentityRepo }

func (i identityUnderContract) SeedIdentity(_ *testing.T, kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string) {
	i.Seed(kind, mbid, xref)
}

func TestIdentityRepo_KeepsTheRepoContract(t *testing.T) {
	contracttest.IdentityRepoContract(t, func(*testing.T) contracttest.IdentityRepoUnderTest {
		return identityUnderContract{&memory.IdentityRepo{}}
	})
}

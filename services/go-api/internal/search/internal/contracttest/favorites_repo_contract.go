package contracttest

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"errors"
	"slices"
	"testing"
)

type FavoritesRepoUnderTest interface {
	ports.FavoritesRepository
	NewUser(t *testing.T) ids.UserId
	SeedFavorites(t *testing.T, user ids.UserId, count int)
}

func FavoritesRepoContract(t *testing.T, newRepo func(t *testing.T) FavoritesRepoUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, FavoritesRepoUnderTest){
		"favorites list newest first":                            contractFavoritesListNewestFirst,
		"adding a favorite again updates it without duplicating": contractFavoriteAddIsAnUpsert,
		"removing a favorite drops only that one":                contractFavoriteRemove,
		"one user's favorites are invisible to another":          contractFavoritesAreUserScoped,
		"a full list refuses a new favorite":                     contractFavoritesFullRefusesNew,
		"a full list still updates a favorite it holds":          contractFavoritesFullUpdatesExisting,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newRepo(t)) })
	}
}

func favoriteKeys(favorites []domain.Favorite) []string {
	keys := make([]string, len(favorites))
	for i, f := range favorites {
		keys[i] = f.Key
	}
	return keys
}

func addFavoriteOrFail(t *testing.T, r FavoritesRepoUnderTest, user ids.UserId, fav domain.Favorite) {
	t.Helper()
	if err := r.Add(t.Context(), user, fav); err != nil {
		t.Fatalf("Add(%q): %v", fav.Key, err)
	}
}

func listFavoritesOrFail(t *testing.T, r FavoritesRepoUnderTest, user ids.UserId) []domain.Favorite {
	t.Helper()
	got, err := r.ListForUser(t.Context(), user)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	return got
}

func albumFav(key, title string) domain.Favorite {
	return domain.Favorite{Kind: domain.ResultKindAlbum, Key: key, Title: title}
}

func contractFavoritesListNewestFirst(t *testing.T, r FavoritesRepoUnderTest) {
	user := r.NewUser(t)
	for _, key := range []string{"first", "second", "third"} {
		addFavoriteOrFail(t, r, user, albumFav(key, key))
	}

	if got, want := favoriteKeys(listFavoritesOrFail(t, r, user)), []string{"third", "second", "first"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
}

func contractFavoriteAddIsAnUpsert(t *testing.T, r FavoritesRepoUnderTest) {
	user := r.NewUser(t)
	addFavoriteOrFail(t, r, user, albumFav("k", "Old"))
	addFavoriteOrFail(t, r, user, domain.Favorite{Kind: domain.ResultKindAlbum, Key: "k", Title: "New", Subtitle: "Sub", ImageURL: "http://img"})

	got := listFavoritesOrFail(t, r, user)
	if len(got) != 1 || got[0].Title != "New" || got[0].Subtitle != "Sub" || got[0].ImageURL != "http://img" {
		t.Errorf("list = %+v, want one favorite carrying the latest fields", got)
	}
}

func contractFavoriteRemove(t *testing.T, r FavoritesRepoUnderTest) {
	user := r.NewUser(t)
	addFavoriteOrFail(t, r, user, albumFav("keep", "Keep"))
	addFavoriteOrFail(t, r, user, albumFav("drop", "Drop"))

	if err := r.Remove(t.Context(), user, domain.ResultKindAlbum, "drop"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := r.Remove(t.Context(), user, domain.ResultKindAlbum, "never-added"); err != nil {
		t.Errorf("Remove of an absent favorite: %v, want nil", err)
	}

	if got, want := favoriteKeys(listFavoritesOrFail(t, r, user)), []string{"keep"}; !slices.Equal(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
}

func contractFavoritesAreUserScoped(t *testing.T, r FavoritesRepoUnderTest) {
	alice, bob := r.NewUser(t), r.NewUser(t)
	addFavoriteOrFail(t, r, alice, albumFav("k", "Alice's"))

	if got := listFavoritesOrFail(t, r, bob); len(got) != 0 {
		t.Errorf("bob sees %+v, want nothing", got)
	}
	if err := r.Remove(t.Context(), bob, domain.ResultKindAlbum, "k"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := listFavoritesOrFail(t, r, alice); len(got) != 1 {
		t.Errorf("alice has %d favorites after bob's remove, want 1", len(got))
	}
}

func contractFavoritesFullRefusesNew(t *testing.T, r FavoritesRepoUnderTest) {
	user := r.NewUser(t)
	r.SeedFavorites(t, user, ports.MaxFavoritesPerUser)

	err := r.Add(t.Context(), user, albumFav("one-too-many", "x"))

	if !errors.Is(err, domain.ErrFavoritesFull) {
		t.Errorf("Add past the cap = %v, want ErrFavoritesFull", err)
	}
}

func contractFavoritesFullUpdatesExisting(t *testing.T, r FavoritesRepoUnderTest) {
	user := r.NewUser(t)
	addFavoriteOrFail(t, r, user, albumFav("held", "Old"))
	r.SeedFavorites(t, user, ports.MaxFavoritesPerUser-1)

	if err := r.Add(t.Context(), user, albumFav("held", "New")); err != nil {
		t.Errorf("Add of a held favorite at the cap = %v, want nil", err)
	}
}

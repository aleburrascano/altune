package favorites_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	shareddomain "altune/go-api/internal/shared/domain"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const (
	favoriteLiftWindow       = 40
	maxFavoriteTextRunes     = 200
	maxFavoriteImageURLBytes = 2048
	everyResultOnOnePage     = 50
	liftQuery                = "no idea"
)

func noIdeaBy(artist string, popularity float64) discovery.SearchResult {
	return discovery.DeezerTrack("No Idea", artist, popularity)
}

func threeNoIdeaTracks() []discovery.SearchResult {
	return []discovery.SearchResult{
		noIdeaBy("Cover Band", 90),
		noIdeaBy("Some DJ", 50),
		noIdeaBy("Don Toliver", 10),
	}
}

func searchFor(t *testing.T, svc *discovery.SearchService, user shared.UserId) []string {
	t.Helper()
	q, err := discovery.NewSearchQuery(liftQuery, map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}, everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	out, err := svc.Execute(context.Background(), user, q, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return discovery.Subtitles(out.Results)
}

func rankedWithFavorites(t *testing.T, repo *discovery.FakeFavoritesRepo, results []discovery.SearchResult) []string {
	t.Helper()
	bg := discoverytest.NewTracker(t)
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithFavorites(repo))
	return searchFor(t, svc, discovery.NewUser())
}

func favoritesRepoOf(favorites ...discovery.Favorite) *discovery.FakeFavoritesRepo {
	return &discovery.FakeFavoritesRepo{Favorites: favorites}
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSearchLift_WithoutFavoritesTheRankingStands(t *testing.T) {
	got := rankedWithFavorites(t, favoritesRepoOf(), threeNoIdeaTracks())

	assertOrder(t, got, []string{"Cover Band", "Some DJ", "Don Toliver"})
}

func TestSearchLift_FavoritedArtistLiftsTheirTracks(t *testing.T) {
	repo := favoritesRepoOf(discovery.FavoriteOf(discovery.ResultKindArtist, "Don Toliver", ""))

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Don Toliver", "Cover Band", "Some DJ"})
}

func TestSearchLift_FavoritedTrackLiftsItself(t *testing.T) {
	repo := favoritesRepoOf(discovery.FavoriteOf(discovery.ResultKindTrack, "No Idea", "Don Toliver"))

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Don Toliver", "Cover Band", "Some DJ"})
}

func TestSearchLift_LeavesOrderAloneWhenFavoritesFailToLoad(t *testing.T) {
	repo := &discovery.FakeFavoritesRepo{
		Favorites: []discovery.Favorite{discovery.FavoriteOf(discovery.ResultKindArtist, "Don Toliver", "")},
		Err:       errors.New("boom"),
	}

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Cover Band", "Some DJ", "Don Toliver"})
}

func tracksEndingWithDistant(count int) []discovery.SearchResult {
	results := make([]discovery.SearchResult, count)
	for i := range count - 1 {
		results[i] = noIdeaBy(fmt.Sprintf("Artist %02d", i), float64(95-i))
	}
	results[count-1] = noIdeaBy("Distant", 1)
	return results
}

func TestSearchLift_LiftsAFavoriteOnTheLastPlaceInsideTheWindow(t *testing.T) {
	repo := favoritesRepoOf(discovery.FavoriteOf(discovery.ResultKindArtist, "Distant", ""))
	results := tracksEndingWithDistant(favoriteLiftWindow)

	unlifted := rankedWithFavorites(t, favoritesRepoOf(), results)
	if unlifted[favoriteLiftWindow-1] != "Distant" {
		t.Fatalf("without favorites Distant ranks at %d, want the window's last place %d", slices.Index(unlifted, "Distant"), favoriteLiftWindow-1)
	}

	if got := rankedWithFavorites(t, repo, results); got[0] != "Distant" {
		t.Errorf("a favorite inside the lift window stayed at %d, want 0", slices.Index(got, "Distant"))
	}
}

func TestSearchLift_DoesNotLiftAFavoriteJustOutsideTheWindow(t *testing.T) {
	repo := favoritesRepoOf(discovery.FavoriteOf(discovery.ResultKindArtist, "Distant", ""))
	results := tracksEndingWithDistant(favoriteLiftWindow + 1)

	unlifted := rankedWithFavorites(t, favoritesRepoOf(), results)
	if unlifted[favoriteLiftWindow] != "Distant" {
		t.Fatalf("without favorites Distant ranks at %d, want just past the window at %d", slices.Index(unlifted, "Distant"), favoriteLiftWindow)
	}

	assertOrder(t, rankedWithFavorites(t, repo, results), unlifted)
}

func TestSearchLift_OneUsersFavoritesDoNotReorderTheCachedRankingOthersSee(t *testing.T) {
	bg := discoverytest.NewTracker(t)
	repo := favoritesRepoOf(discovery.FavoriteOf(discovery.ResultKindArtist, "Don Toliver", ""))
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: threeNoIdeaTracks()}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, discovery.NewCircuitBreaker(),
		discovery.WithFavorites(repo), discovery.WithResultCache(discovery.NewFakeResultCache()))

	unlifted := []string{"Cover Band", "Some DJ", "Don Toliver"}
	assertOrder(t, searchFor(t, svc, shared.SystemUserId()), unlifted)
	assertOrder(t, searchFor(t, svc, discovery.NewUser()), []string{"Don Toliver", "Cover Band", "Some DJ"})
	assertOrder(t, searchFor(t, svc, shared.SystemUserId()), unlifted)
}

func TestFavoritesService_AddNormalizesKey(t *testing.T) {
	repo := &discovery.FakeFavoritesRepo{}
	svc := discovery.NewFavoritesService(repo)

	err := svc.Add(context.Background(), shared.UserId{}, discovery.Favorite{
		Kind:  discovery.ResultKindArtist,
		Title: "  Don  Tóliver ",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if want := discovery.FavoriteOf(discovery.ResultKindArtist, "Don Toliver", "").Key; repo.Added[0].Key != want {
		t.Errorf("key = %q, want %q", repo.Added[0].Key, want)
	}
}

func TestFavoritesService_AddRejectsEmptyTitle(t *testing.T) {
	svc := discovery.NewFavoritesService(&discovery.FakeFavoritesRepo{})
	if err := svc.Add(context.Background(), shared.UserId{}, discovery.Favorite{Kind: discovery.ResultKindArtist}); err == nil {
		t.Error("expected an error for a favorite with no title")
	}
}

func assertTypedError(t *testing.T, err error, wantKind shareddomain.Kind, wantCode string) {
	t.Helper()
	var typed *shareddomain.CodedError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a typed %s %s", err, wantKind, wantCode)
	}
	if typed.Code().Kind() != wantKind || typed.Code().String() != wantCode {
		t.Fatalf("err = %s %s, want %s %s", typed.Code().Kind(), typed.Code(), wantKind, wantCode)
	}
}

func assertRejectedWithoutWriting(t *testing.T, fav discovery.Favorite) {
	t.Helper()
	repo := &discovery.FakeFavoritesRepo{}
	err := discovery.NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav)

	assertTypedError(t, err, shareddomain.KindInvalid, "discovery.invalid_favorite")
	if len(repo.Added) != 0 {
		t.Errorf("repo.Add called %d times for a rejected favorite", len(repo.Added))
	}
}

func TestFavoritesAdd_RejectsOversizeOrUnsafeFieldsWithoutWriting(t *testing.T) {
	longText := strings.Repeat("é", maxFavoriteTextRunes+1)
	longURL := "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes)
	cases := map[string]discovery.Favorite{
		"title over cap":            {Kind: discovery.ResultKindAlbum, Title: longText, Subtitle: "Kendrick Lamar"},
		"subtitle over cap":         {Kind: discovery.ResultKindAlbum, Title: "DAMN.", Subtitle: longText},
		"image_url over cap":        {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: longURL},
		"image_url http":            {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: "http://img.example/a.jpg"},
		"image_url script":          {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: "javascript:alert(1)"},
		"image_url no host":         {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: "https:///a.jpg"},
		"image_url data":            {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: "data:image/png;base64,AAAA"},
		"image_url scheme-relative": {Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: "//img.example/a.jpg"},
		"title with NUL":            {Kind: discovery.ResultKindAlbum, Title: "a\x00", Subtitle: "Kendrick Lamar"},
		"subtitle with NUL":         {Kind: discovery.ResultKindAlbum, Title: "DAMN.", Subtitle: "a\x00b"},
		"title invalid UTF-8":       {Kind: discovery.ResultKindAlbum, Title: "a\xffb"},
		"empty normalized key":      {Kind: discovery.ResultKindArtist, Title: "!!!"},
	}
	for name, fav := range cases {
		t.Run(name, func(t *testing.T) {
			assertRejectedWithoutWriting(t, fav)
		})
	}
}

func TestFavoritesAdd_AcceptsFieldsAtTheirCaps(t *testing.T) {
	repo := &discovery.FakeFavoritesRepo{}
	fav := discovery.Favorite{
		Kind:     discovery.ResultKindAlbum,
		Title:    strings.Repeat("é", maxFavoriteTextRunes),
		Subtitle: strings.Repeat("b", maxFavoriteTextRunes),
		ImageURL: "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes-len("https://img.example/")),
	}

	if err := discovery.NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav); err != nil {
		t.Fatalf("Add at the caps: %v", err)
	}
	if len(repo.Added) != 1 {
		t.Fatalf("repo.Add called %d times, want 1", len(repo.Added))
	}
}

func TestFavoritesAdd_StoresExactlyTheImageURLsSearchWouldShow(t *testing.T) {
	shown := []string{
		"https://img.example/a.jpg",
		"https://img.example/a.jpg?w=300#x",
		"HTTPS://img.example/a.jpg",
		"https://img.example:8443/a.jpg",
	}
	hidden := []string{
		"https://",
		"//img.example/a.jpg",
		"https:img.example/a.jpg",
		"ftp://img.example/a.jpg",
		"httpss://img.example/a.jpg",
		" https://img.example/a.jpg",
		"http://img.example/a.jpg",
	}
	for _, imageURL := range shown {
		t.Run(imageURL, func(t *testing.T) {
			repo := &discovery.FakeFavoritesRepo{}
			fav := discovery.Favorite{Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: imageURL}
			if err := discovery.NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav); err != nil {
				t.Fatalf("Add rejected an image_url search shows: %v", err)
			}
		})
	}
	for _, imageURL := range hidden {
		t.Run(imageURL, func(t *testing.T) {
			assertRejectedWithoutWriting(t, discovery.Favorite{Kind: discovery.ResultKindAlbum, Title: "DAMN.", ImageURL: imageURL})
		})
	}
}

func TestFavoritesRemove_OversizeTitleStillDeletes(t *testing.T) {
	repo := &discovery.FakeFavoritesRepo{}
	err := discovery.NewFavoritesService(repo).Remove(context.Background(), shared.UserId{},
		discovery.ResultKindAlbum, strings.Repeat("a", maxFavoriteTextRunes+1), "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(repo.Removed) != 1 {
		t.Errorf("repo.Remove called %d times, want 1", len(repo.Removed))
	}
}

func TestFavoritesAdd_OverThePerUserCapIsATypedConflict(t *testing.T) {
	repo := &discovery.FakeFavoritesRepo{Err: fmt.Errorf("insert favorite: %w", discovery.ErrFavoritesFull)}
	err := discovery.NewFavoritesService(repo).Add(context.Background(), shared.UserId{},
		discovery.Favorite{Kind: discovery.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"})

	assertTypedError(t, err, shareddomain.KindConflict, "discovery.favorites_full")
}

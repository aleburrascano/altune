package favorites_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/ids"
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

func noIdeaBy(artist string, popularity float64) search.SearchResult {
	return search.DeezerTrack("No Idea", artist, popularity)
}

func threeNoIdeaTracks() []search.SearchResult {
	return []search.SearchResult{
		noIdeaBy("Cover Band", 90),
		noIdeaBy("Some DJ", 50),
		noIdeaBy("Don Toliver", 10),
	}
}

func searchFor(t *testing.T, svc *search.SearchService, user ids.UserId) []string {
	t.Helper()
	q, err := search.NewSearchQuery(liftQuery, map[search.ResultKind]bool{search.ResultKindTrack: true}, everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	out, err := svc.Execute(context.Background(), user, q)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return search.Subtitles(out.Results)
}

func rankedWithFavorites(t *testing.T, repo *search.FakeFavoritesRepo, results []search.SearchResult) []string {
	t.Helper()
	bg := searchtest.NewTracker(t)
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: results}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithFavorites(repo))
	return searchFor(t, svc, search.NewUser())
}

func favoritesRepoOf(favorites ...search.Favorite) *search.FakeFavoritesRepo {
	return &search.FakeFavoritesRepo{Favorites: favorites}
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
	repo := favoritesRepoOf(search.FavoriteOf(search.ResultKindArtist, "Don Toliver", ""))

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Don Toliver", "Cover Band", "Some DJ"})
}

func TestSearchLift_FavoritedTrackLiftsItself(t *testing.T) {
	repo := favoritesRepoOf(search.FavoriteOf(search.ResultKindTrack, "No Idea", "Don Toliver"))

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Don Toliver", "Cover Band", "Some DJ"})
}

func TestSearchLift_LeavesOrderAloneWhenFavoritesFailToLoad(t *testing.T) {
	repo := &search.FakeFavoritesRepo{
		Favorites: []search.Favorite{search.FavoriteOf(search.ResultKindArtist, "Don Toliver", "")},
		Err:       errors.New("boom"),
	}

	got := rankedWithFavorites(t, repo, threeNoIdeaTracks())

	assertOrder(t, got, []string{"Cover Band", "Some DJ", "Don Toliver"})
}

func tracksEndingWithDistant(count int) []search.SearchResult {
	results := make([]search.SearchResult, count)
	for i := range count - 1 {
		results[i] = noIdeaBy(fmt.Sprintf("Artist %02d", i), float64(95-i))
	}
	results[count-1] = noIdeaBy("Distant", 1)
	return results
}

func TestSearchLift_LiftsAFavoriteOnTheLastPlaceInsideTheWindow(t *testing.T) {
	repo := favoritesRepoOf(search.FavoriteOf(search.ResultKindArtist, "Distant", ""))
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
	repo := favoritesRepoOf(search.FavoriteOf(search.ResultKindArtist, "Distant", ""))
	results := tracksEndingWithDistant(favoriteLiftWindow + 1)

	unlifted := rankedWithFavorites(t, favoritesRepoOf(), results)
	if unlifted[favoriteLiftWindow] != "Distant" {
		t.Fatalf("without favorites Distant ranks at %d, want just past the window at %d", slices.Index(unlifted, "Distant"), favoriteLiftWindow)
	}

	assertOrder(t, rankedWithFavorites(t, repo, results), unlifted)
}

func TestSearchLift_OneUsersFavoritesDoNotReorderTheCachedRankingOthersSee(t *testing.T) {
	bg := searchtest.NewTracker(t)
	repo := favoritesRepoOf(search.FavoriteOf(search.ResultKindArtist, "Don Toliver", ""))
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: threeNoIdeaTracks()}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(),
		search.WithFavorites(repo), search.WithResultCache(search.NewFakeResultCache()))

	unlifted := []string{"Cover Band", "Some DJ", "Don Toliver"}
	assertOrder(t, searchFor(t, svc, ids.SystemUserId()), unlifted)
	assertOrder(t, searchFor(t, svc, search.NewUser()), []string{"Don Toliver", "Cover Band", "Some DJ"})
	assertOrder(t, searchFor(t, svc, ids.SystemUserId()), unlifted)
}

func TestFavoritesService_AddNormalizesKey(t *testing.T) {
	repo := &search.FakeFavoritesRepo{}
	svc := search.NewFavoritesService(repo)

	err := svc.Add(context.Background(), ids.UserId{}, search.Favorite{
		Kind:  search.ResultKindArtist,
		Title: "  Don  Tóliver ",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if want := search.FavoriteOf(search.ResultKindArtist, "Don Toliver", "").Key; repo.Added[0].Key != want {
		t.Errorf("key = %q, want %q", repo.Added[0].Key, want)
	}
}

func TestFavoritesService_AddRejectsEmptyTitle(t *testing.T) {
	svc := search.NewFavoritesService(&search.FakeFavoritesRepo{})
	if err := svc.Add(context.Background(), ids.UserId{}, search.Favorite{Kind: search.ResultKindArtist}); err == nil {
		t.Error("expected an error for a favorite with no title")
	}
}

func assertTypedError(t *testing.T, err error, wantKind apierror.Kind, wantCode string) {
	t.Helper()
	var typed *apierror.CodedError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a typed %s %s", err, wantKind, wantCode)
	}
	if typed.Code().Kind() != wantKind || typed.Code().String() != wantCode {
		t.Fatalf("err = %s %s, want %s %s", typed.Code().Kind(), typed.Code(), wantKind, wantCode)
	}
}

func assertRejectedWithoutWriting(t *testing.T, fav search.Favorite) {
	t.Helper()
	repo := &search.FakeFavoritesRepo{}
	err := search.NewFavoritesService(repo).Add(context.Background(), ids.UserId{}, fav)

	assertTypedError(t, err, apierror.KindInvalid, "discovery.invalid_favorite")
	if len(repo.Added) != 0 {
		t.Errorf("repo.Add called %d times for a rejected favorite", len(repo.Added))
	}
}

func TestFavoritesAdd_RejectsOversizeOrUnsafeFieldsWithoutWriting(t *testing.T) {
	longText := strings.Repeat("é", maxFavoriteTextRunes+1)
	longURL := "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes)
	cases := map[string]search.Favorite{
		"title over cap":            {Kind: search.ResultKindAlbum, Title: longText, Subtitle: "Kendrick Lamar"},
		"subtitle over cap":         {Kind: search.ResultKindAlbum, Title: "DAMN.", Subtitle: longText},
		"image_url over cap":        {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: longURL},
		"image_url http":            {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: "http://img.example/a.jpg"},
		"image_url script":          {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: "javascript:alert(1)"},
		"image_url no host":         {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: "https:///a.jpg"},
		"image_url data":            {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: "data:image/png;base64,AAAA"},
		"image_url scheme-relative": {Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: "//img.example/a.jpg"},
		"title with NUL":            {Kind: search.ResultKindAlbum, Title: "a\x00", Subtitle: "Kendrick Lamar"},
		"subtitle with NUL":         {Kind: search.ResultKindAlbum, Title: "DAMN.", Subtitle: "a\x00b"},
		"title invalid UTF-8":       {Kind: search.ResultKindAlbum, Title: "a\xffb"},
		"empty normalized key":      {Kind: search.ResultKindArtist, Title: "!!!"},
	}
	for name, fav := range cases {
		t.Run(name, func(t *testing.T) {
			assertRejectedWithoutWriting(t, fav)
		})
	}
}

func TestFavoritesAdd_AcceptsFieldsAtTheirCaps(t *testing.T) {
	repo := &search.FakeFavoritesRepo{}
	fav := search.Favorite{
		Kind:     search.ResultKindAlbum,
		Title:    strings.Repeat("é", maxFavoriteTextRunes),
		Subtitle: strings.Repeat("b", maxFavoriteTextRunes),
		ImageURL: "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes-len("https://img.example/")),
	}

	if err := search.NewFavoritesService(repo).Add(context.Background(), ids.UserId{}, fav); err != nil {
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
			repo := &search.FakeFavoritesRepo{}
			fav := search.Favorite{Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: imageURL}
			if err := search.NewFavoritesService(repo).Add(context.Background(), ids.UserId{}, fav); err != nil {
				t.Fatalf("Add rejected an image_url search shows: %v", err)
			}
		})
	}
	for _, imageURL := range hidden {
		t.Run(imageURL, func(t *testing.T) {
			assertRejectedWithoutWriting(t, search.Favorite{Kind: search.ResultKindAlbum, Title: "DAMN.", ImageURL: imageURL})
		})
	}
}

func TestFavoritesRemove_OversizeTitleStillDeletes(t *testing.T) {
	repo := &search.FakeFavoritesRepo{}
	err := search.NewFavoritesService(repo).Remove(context.Background(), ids.UserId{},
		search.ResultKindAlbum, strings.Repeat("a", maxFavoriteTextRunes+1), "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(repo.Removed) != 1 {
		t.Errorf("repo.Remove called %d times, want 1", len(repo.Removed))
	}
}

func TestFavoritesAdd_OverThePerUserCapIsATypedConflict(t *testing.T) {
	repo := &search.FakeFavoritesRepo{Err: fmt.Errorf("insert favorite: %w", search.ErrFavoritesFull)}
	err := search.NewFavoritesService(repo).Add(context.Background(), ids.UserId{},
		search.Favorite{Kind: search.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"})

	assertTypedError(t, err, apierror.KindConflict, "discovery.favorites_full")
}

package favorites

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func favResult(kind domain.ResultKind, title, subtitle string) domain.SearchResult {
	return domain.SearchResult{Kind: kind, Title: title, Subtitle: subtitle}
}

func TestLiftFavorites_FavoritedArtistLiftsTheirTracks(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{Favorites: []domain.Favorite{
		discoverytest.FavoriteOf(domain.ResultKindArtist, "Don Toliver", ""),
	}}
	lifter := NewFavoritesLifter(repo)

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea (Remix)", "Some DJ"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	got := discoverytest.Subtitles(lifter.Lift(context.Background(), shared.UserId{}, ranked))
	want := []string{"Don Toliver", "Cover Band", "Some DJ"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if ranked[0].Subtitle != "Cover Band" {
		t.Error("liftFavorites mutated the caller's slice")
	}
}

func TestLiftFavorites_FavoritedTrackLiftsItself(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{Favorites: []domain.Favorite{
		discoverytest.FavoriteOf(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}}
	lifter := NewFavoritesLifter(repo)

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	lifted := lifter.Lift(context.Background(), shared.UserId{}, ranked)
	if lifted[0].Subtitle != "Don Toliver" {
		t.Errorf("top result = %q, want Don Toliver", lifted[0].Subtitle)
	}
}

func TestLiftFavorites_LeavesOrderAloneWithoutFavorites(t *testing.T) {
	lifter := NewFavoritesLifter(&discoverytest.FakeFavoritesRepo{})
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := discoverytest.Subtitles(lifter.Lift(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed with no favorites: %v", got)
	}
}

func TestLiftFavorites_LeavesOrderAloneOnRepoError(t *testing.T) {
	lifter := NewFavoritesLifter(&discoverytest.FakeFavoritesRepo{Err: errors.New("boom")})
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := discoverytest.Subtitles(lifter.Lift(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed on repo error: %v", got)
	}
}

func TestLiftFavorites_DoesNotLiftBeyondTheWindow(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{Favorites: []domain.Favorite{
		discoverytest.FavoriteOf(domain.ResultKindArtist, "Distant", ""),
	}}
	lifter := NewFavoritesLifter(repo)

	ranked := make([]domain.SearchResult, favoriteLiftWindow+5)
	for i := range ranked {
		ranked[i] = favResult(domain.ResultKindTrack, "T", "Other")
	}
	ranked[favoriteLiftWindow+2] = favResult(domain.ResultKindTrack, "T", "Distant")

	lifted := lifter.Lift(context.Background(), shared.UserId{}, ranked)
	if lifted[0].Subtitle == "Distant" {
		t.Error("a favorite outside the lift window was pulled to the top")
	}
	if lifted[favoriteLiftWindow+2].Subtitle != "Distant" {
		t.Error("a favorite outside the lift window moved")
	}
}

func TestFavoritesService_AddNormalizesKey(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{}
	svc := NewFavoritesService(repo)

	err := svc.Add(context.Background(), shared.UserId{}, domain.Favorite{
		Kind:  domain.ResultKindArtist,
		Title: "  Don  Tóliver ",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if repo.Added[0].Key != domain.FavoriteKey(domain.ResultKindArtist, "Don Toliver", "") {
		t.Errorf("key = %q", repo.Added[0].Key)
	}
}

func TestFavoritesService_AddRejectsEmptyTitle(t *testing.T) {
	svc := NewFavoritesService(&discoverytest.FakeFavoritesRepo{})
	if err := svc.Add(context.Background(), shared.UserId{}, domain.Favorite{Kind: domain.ResultKindArtist}); err == nil {
		t.Error("expected an error for a favorite with no title")
	}
}

func assertTypedError(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	var typed *domain.CodedError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a typed %d %s", err, wantStatus, wantCode)
	}
	if typed.HTTPStatus() != wantStatus || typed.ErrorCode() != wantCode {
		t.Fatalf("err = %d %s, want %d %s", typed.HTTPStatus(), typed.ErrorCode(), wantStatus, wantCode)
	}
}

func TestFavoritesAdd_RejectsOversizeOrUnsafeFieldsWithoutWriting(t *testing.T) {
	longText := strings.Repeat("é", maxFavoriteTextRunes+1)
	longURL := "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes)
	cases := map[string]domain.Favorite{
		"title over cap":            {Kind: domain.ResultKindAlbum, Title: longText, Subtitle: "Kendrick Lamar"},
		"subtitle over cap":         {Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: longText},
		"image_url over cap":        {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: longURL},
		"image_url http":            {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "http://img.example/a.jpg"},
		"image_url script":          {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "javascript:alert(1)"},
		"image_url no host":         {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "https:///a.jpg"},
		"image_url data":            {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "data:image/png;base64,AAAA"},
		"image_url scheme-relative": {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "//img.example/a.jpg"},
		"title with NUL":            {Kind: domain.ResultKindAlbum, Title: "a\x00", Subtitle: "Kendrick Lamar"},
		"subtitle with NUL":         {Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: "a\x00b"},
		"title invalid UTF-8":       {Kind: domain.ResultKindAlbum, Title: "a\xffb"},
		"empty normalized key":      {Kind: domain.ResultKindArtist, Title: "!!!"},
	}
	for name, fav := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &discoverytest.FakeFavoritesRepo{}
			err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav)

			assertTypedError(t, err, http.StatusBadRequest, "discovery.invalid_favorite")
			if len(repo.Added) != 0 {
				t.Errorf("repo.Add called %d times for a rejected favorite", len(repo.Added))
			}
		})
	}
}

func TestFavoritesAdd_AcceptsFieldsAtTheirCaps(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{}
	fav := domain.Favorite{
		Kind:     domain.ResultKindAlbum,
		Title:    strings.Repeat("é", maxFavoriteTextRunes),
		Subtitle: strings.Repeat("b", maxFavoriteTextRunes),
		ImageURL: "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes-len("https://img.example/")),
	}

	if err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav); err != nil {
		t.Fatalf("Add at the caps: %v", err)
	}
	if len(repo.Added) != 1 {
		t.Fatalf("repo.Add called %d times, want 1", len(repo.Added))
	}
}

func TestFavoritesRemove_OversizeTitleStillDeletes(t *testing.T) {
	repo := &discoverytest.FakeFavoritesRepo{}
	err := NewFavoritesService(repo).Remove(context.Background(), shared.UserId{},
		domain.ResultKindAlbum, strings.Repeat("a", maxFavoriteTextRunes+1), "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(repo.Removed) != 1 {
		t.Errorf("repo.Remove called %d times, want 1", len(repo.Removed))
	}
}

type fullFavoritesRepo struct {
	discoverytest.FakeFavoritesRepo
}

func (f *fullFavoritesRepo) Add(context.Context, shared.UserId, domain.Favorite) error {
	return fmt.Errorf("insert favorite: %w", ports.ErrFavoritesFull)
}

func TestFavoritesAdd_OverThePerUserCapIsATypedConflict(t *testing.T) {
	err := NewFavoritesService(&fullFavoritesRepo{}).Add(context.Background(), shared.UserId{},
		domain.Favorite{Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"})

	assertTypedError(t, err, http.StatusConflict, "discovery.favorites_full")
}

func TestValidateFavoriteImageURL_AgreesWithSafeImageURL(t *testing.T) {
	urls := []string{
		"https://img.example/a.jpg",
		"https://img.example/a.jpg?w=300#x",
		"HTTPS://img.example/a.jpg",
		"https://img.example:8443/a.jpg",
		"https://",
		"//img.example/a.jpg",
		"https:img.example/a.jpg",
		"ftp://img.example/a.jpg",
		"httpss://img.example/a.jpg",
		" https://img.example/a.jpg",
		"http://img.example/a.jpg",
	}
	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			err := validateFavoriteImageURL(raw)
			if rejected := domain.SafeImageURL(raw) == ""; (err != nil) != rejected {
				t.Errorf("validateFavoriteImageURL(%q) err = %v, but SafeImageURL rejected = %v", raw, err, rejected)
			}
		})
	}
}

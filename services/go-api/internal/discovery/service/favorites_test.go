package service

import (
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

type fakeFavoritesRepo struct {
	favorites []domain.Favorite
	err       error
	added     []domain.Favorite
	removed   []string
	listCalls int
}

func (f *fakeFavoritesRepo) Add(_ context.Context, _ shared.UserId, fav domain.Favorite) error {
	f.added = append(f.added, fav)
	return f.err
}

func (f *fakeFavoritesRepo) Remove(_ context.Context, _ shared.UserId, kind domain.ResultKind, key string) error {
	f.removed = append(f.removed, kind.String()+"|"+key)
	return f.err
}

func (f *fakeFavoritesRepo) ListForUser(_ context.Context, _ shared.UserId) ([]domain.Favorite, error) {
	f.listCalls++
	return f.favorites, f.err
}

func favResult(kind domain.ResultKind, title, subtitle string) domain.SearchResult {
	return domain.SearchResult{Kind: kind, Title: title, Subtitle: subtitle}
}

func subtitles(results []domain.SearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Subtitle
	}
	return out
}

func favoriteOf(kind domain.ResultKind, title, subtitle string) domain.Favorite {
	return domain.Favorite{
		Kind:     kind,
		Key:      domain.FavoriteKey(kind, title, subtitle),
		Title:    title,
		Subtitle: subtitle,
	}
}

func TestLiftFavorites_FavoritedArtistLiftsTheirTracks(t *testing.T) {
	repo := &fakeFavoritesRepo{favorites: []domain.Favorite{
		favoriteOf(domain.ResultKindArtist, "Don Toliver", ""),
	}}
	s := &Service{favorites: newFavoritesLifter(repo)}

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea (Remix)", "Some DJ"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	got := subtitles(s.favorites.lift(context.Background(), shared.UserId{}, ranked))
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
	repo := &fakeFavoritesRepo{favorites: []domain.Favorite{
		favoriteOf(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}}
	s := &Service{favorites: newFavoritesLifter(repo)}

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	lifted := s.favorites.lift(context.Background(), shared.UserId{}, ranked)
	if lifted[0].Subtitle != "Don Toliver" {
		t.Errorf("top result = %q, want Don Toliver", lifted[0].Subtitle)
	}
}

func TestLiftFavorites_LeavesOrderAloneWithoutFavorites(t *testing.T) {
	s := &Service{favorites: newFavoritesLifter(&fakeFavoritesRepo{})}
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := subtitles(s.favorites.lift(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed with no favorites: %v", got)
	}
}

func TestLiftFavorites_LeavesOrderAloneOnRepoError(t *testing.T) {
	s := &Service{favorites: newFavoritesLifter(&fakeFavoritesRepo{err: errors.New("boom")})}
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := subtitles(s.favorites.lift(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed on repo error: %v", got)
	}
}

func TestLiftFavorites_DoesNotLiftBeyondTheWindow(t *testing.T) {
	repo := &fakeFavoritesRepo{favorites: []domain.Favorite{
		favoriteOf(domain.ResultKindArtist, "Distant", ""),
	}}
	s := &Service{favorites: newFavoritesLifter(repo)}

	ranked := make([]domain.SearchResult, favoriteLiftWindow+5)
	for i := range ranked {
		ranked[i] = favResult(domain.ResultKindTrack, "T", "Other")
	}
	ranked[favoriteLiftWindow+2] = favResult(domain.ResultKindTrack, "T", "Distant")

	lifted := s.favorites.lift(context.Background(), shared.UserId{}, ranked)
	if lifted[0].Subtitle == "Distant" {
		t.Error("a favorite outside the lift window was pulled to the top")
	}
	if lifted[favoriteLiftWindow+2].Subtitle != "Distant" {
		t.Error("a favorite outside the lift window moved")
	}
}

func TestFavoritesService_AddNormalizesKey(t *testing.T) {
	repo := &fakeFavoritesRepo{}
	svc := NewFavoritesService(repo)

	err := svc.Add(context.Background(), shared.UserId{}, domain.Favorite{
		Kind:  domain.ResultKindArtist,
		Title: "  Don  Tóliver ",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if repo.added[0].Key != domain.FavoriteKey(domain.ResultKindArtist, "Don Toliver", "") {
		t.Errorf("key = %q", repo.added[0].Key)
	}
}

func TestFavoritesService_AddRejectsEmptyTitle(t *testing.T) {
	svc := NewFavoritesService(&fakeFavoritesRepo{})
	if err := svc.Add(context.Background(), shared.UserId{}, domain.Favorite{Kind: domain.ResultKindArtist}); err == nil {
		t.Error("expected an error for a favorite with no title")
	}
}

type typedHTTPError interface {
	HTTPStatus() int
	ErrorCode() string
}

func assertTypedError(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	var typed typedHTTPError
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
		"title over cap":       {Kind: domain.ResultKindAlbum, Title: longText, Subtitle: "Kendrick Lamar"},
		"subtitle over cap":    {Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: longText},
		"image_url over cap":   {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: longURL},
		"image_url http":       {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "http://img.example/a.jpg"},
		"image_url script":     {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "javascript:alert(1)"},
		"image_url no host":    {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "https:///a.jpg"},
		"empty normalized key": {Kind: domain.ResultKindArtist, Title: "!!!"},
	}
	for name, fav := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeFavoritesRepo{}
			err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav)

			assertTypedError(t, err, http.StatusBadRequest, "discovery.invalid_favorite")
			if len(repo.added) != 0 {
				t.Errorf("repo.Add called %d times for a rejected favorite", len(repo.added))
			}
		})
	}
}

func TestFavoritesAdd_AcceptsFieldsAtTheirCaps(t *testing.T) {
	repo := &fakeFavoritesRepo{}
	fav := domain.Favorite{
		Kind:     domain.ResultKindAlbum,
		Title:    strings.Repeat("é", maxFavoriteTextRunes),
		Subtitle: strings.Repeat("b", maxFavoriteTextRunes),
		ImageURL: "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes-len("https://img.example/")),
	}

	if err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav); err != nil {
		t.Fatalf("Add at the caps: %v", err)
	}
	if len(repo.added) != 1 {
		t.Fatalf("repo.Add called %d times, want 1", len(repo.added))
	}
}

func TestFavoritesRemove_OversizeTitleStillDeletes(t *testing.T) {
	repo := &fakeFavoritesRepo{}
	err := NewFavoritesService(repo).Remove(context.Background(), shared.UserId{},
		domain.ResultKindAlbum, strings.Repeat("a", maxFavoriteTextRunes+1), "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(repo.removed) != 1 {
		t.Errorf("repo.Remove called %d times, want 1", len(repo.removed))
	}
}

type fullFavoritesRepo struct{ fakeFavoritesRepo }

func (f *fullFavoritesRepo) Add(context.Context, shared.UserId, domain.Favorite) error {
	return fmt.Errorf("insert favorite: %w", ports.ErrFavoritesFull)
}

func TestFavoritesAdd_OverThePerUserCapIsATypedConflict(t *testing.T) {
	err := NewFavoritesService(&fullFavoritesRepo{}).Add(context.Background(), shared.UserId{},
		domain.Favorite{Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"})

	assertTypedError(t, err, http.StatusConflict, "discovery.favorites_full")
}

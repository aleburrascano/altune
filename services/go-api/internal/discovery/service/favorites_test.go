package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

type fakeFavoritesRepo struct {
	favorites []domain.Favorite
	err       error
	added     []domain.Favorite
	removed   []string
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
	s := &Service{favoritesRepo: repo}

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea (Remix)", "Some DJ"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	got := subtitles(s.liftFavorites(context.Background(), shared.UserId{}, ranked))
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
	s := &Service{favoritesRepo: repo}

	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "No Idea", "Cover Band"),
		favResult(domain.ResultKindTrack, "No Idea", "Don Toliver"),
	}

	lifted := s.liftFavorites(context.Background(), shared.UserId{}, ranked)
	if lifted[0].Subtitle != "Don Toliver" {
		t.Errorf("top result = %q, want Don Toliver", lifted[0].Subtitle)
	}
}

func TestLiftFavorites_LeavesOrderAloneWithoutFavorites(t *testing.T) {
	s := &Service{favoritesRepo: &fakeFavoritesRepo{}}
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := subtitles(s.liftFavorites(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed with no favorites: %v", got)
	}
}

func TestLiftFavorites_LeavesOrderAloneOnRepoError(t *testing.T) {
	s := &Service{favoritesRepo: &fakeFavoritesRepo{err: errors.New("boom")}}
	ranked := []domain.SearchResult{
		favResult(domain.ResultKindTrack, "A", "x"),
		favResult(domain.ResultKindTrack, "B", "y"),
	}

	if got := subtitles(s.liftFavorites(context.Background(), shared.UserId{}, ranked)); got[0] != "x" || got[1] != "y" {
		t.Errorf("order changed on repo error: %v", got)
	}
}

func TestLiftFavorites_DoesNotLiftBeyondTheWindow(t *testing.T) {
	repo := &fakeFavoritesRepo{favorites: []domain.Favorite{
		favoriteOf(domain.ResultKindArtist, "Distant", ""),
	}}
	s := &Service{favoritesRepo: repo}

	ranked := make([]domain.SearchResult, favoriteLiftWindow+5)
	for i := range ranked {
		ranked[i] = favResult(domain.ResultKindTrack, "T", "Other")
	}
	ranked[favoriteLiftWindow+2] = favResult(domain.ResultKindTrack, "T", "Distant")

	lifted := s.liftFavorites(context.Background(), shared.UserId{}, ranked)
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

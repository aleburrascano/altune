package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
)

type FakeFavoritesRepo struct {
	Favorites []domain.Favorite
	Err       error
	Added     []domain.Favorite
	Removed   []string
	ListCalls int
}

func (f *FakeFavoritesRepo) Add(_ context.Context, _ shared.UserId, fav domain.Favorite) error {
	f.Added = append(f.Added, fav)
	return f.Err
}

func (f *FakeFavoritesRepo) Remove(_ context.Context, _ shared.UserId, kind domain.ResultKind, key string) error {
	f.Removed = append(f.Removed, kind.String()+"|"+key)
	return f.Err
}

func (f *FakeFavoritesRepo) ListForUser(_ context.Context, _ shared.UserId) ([]domain.Favorite, error) {
	f.ListCalls++
	return f.Favorites, f.Err
}

func Subtitles(results []domain.SearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Subtitle
	}
	return out
}

func FavoriteOf(kind domain.ResultKind, title, subtitle string) domain.Favorite {
	return domain.Favorite{
		Kind:     kind,
		Key:      domain.FavoriteKey(kind, title, subtitle),
		Title:    title,
		Subtitle: subtitle,
	}
}

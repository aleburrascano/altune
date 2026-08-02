package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

const favoriteLiftWindow = 40

type FavoritesService struct {
	repo ports.FavoritesRepository
}

func NewFavoritesService(repo ports.FavoritesRepository) *FavoritesService {
	return &FavoritesService{repo: repo}
}

func (s *FavoritesService) Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error {
	if s.repo == nil {
		return nil
	}
	fav.Key = domain.FavoriteKey(fav.Kind, fav.Title, fav.Subtitle)
	if fav.Key == "" || fav.Key == "|" {
		return fmt.Errorf("favorite needs a title")
	}
	if err := s.repo.Add(ctx, userId, fav); err != nil {
		return fmt.Errorf("add favorite: %w", err)
	}
	return nil
}

func (s *FavoritesService) Remove(ctx context.Context, userId shared.UserId, kind domain.ResultKind, title, subtitle string) error {
	if s.repo == nil {
		return nil
	}
	if err := s.repo.Remove(ctx, userId, kind, domain.FavoriteKey(kind, title, subtitle)); err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	return nil
}

func (s *FavoritesService) List(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error) {
	if s.repo == nil {
		return nil, nil
	}
	favorites, err := s.repo.ListForUser(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("list favorites: %w", err)
	}
	return favorites, nil
}

type favoriteSet struct {
	byKind  map[string]bool
	artists map[string]bool
}

func newFavoriteSet(favorites []domain.Favorite) favoriteSet {
	set := favoriteSet{byKind: map[string]bool{}, artists: map[string]bool{}}
	for _, f := range favorites {
		set.byKind[f.Kind.String()+"|"+f.Key] = true
		if f.Kind == domain.ResultKindArtist {
			set.artists[f.Key] = true
		}
	}
	return set
}

func (f favoriteSet) covers(r domain.SearchResult) bool {
	if f.artists[domain.ArtistKeyOf(r)] {
		return true
	}
	return f.byKind[r.Kind.String()+"|"+domain.FavoriteKeyOf(r)]
}

func (s *Service) liftFavorites(
	ctx context.Context,
	userId shared.UserId,
	ranked []domain.SearchResult,
) []domain.SearchResult {
	if s.favoritesRepo == nil || len(ranked) < 2 {
		return ranked
	}
	favorites, err := s.favoritesRepo.ListForUser(ctx, userId)
	if err != nil {
		slog.WarnContext(ctx, "search.v2.favorites_load_failed", "error", err)
		return ranked
	}
	if len(favorites) == 0 {
		return ranked
	}

	set := newFavoriteSet(favorites)
	window := min(favoriteLiftWindow, len(ranked))
	out := slices.Clone(ranked)
	head := out[:window]
	sort.SliceStable(head, func(i, j int) bool {
		return set.covers(head[i]) && !set.covers(head[j])
	})
	return out
}

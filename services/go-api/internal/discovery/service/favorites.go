package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"unicode/utf8"
)

const (
	favoriteLiftWindow       = 40
	maxFavoriteTextRunes     = 200
	maxFavoriteImageURLBytes = 2048
)

type invalidFavoriteError struct{ msg string }

func (e *invalidFavoriteError) Error() string     { return e.msg }
func (e *invalidFavoriteError) HTTPStatus() int   { return 400 }
func (e *invalidFavoriteError) ErrorCode() string { return "discovery.invalid_favorite" }

type favoritesFullError struct{}

func (favoritesFullError) Error() string {
	return fmt.Sprintf("favorites are full: at most %d per account", ports.MaxFavoritesPerUser)
}
func (favoritesFullError) HTTPStatus() int   { return 409 }
func (favoritesFullError) ErrorCode() string { return "discovery.favorites_full" }

func validateFavoriteText(title, subtitle string) error {
	if utf8.RuneCountInString(title) > maxFavoriteTextRunes {
		return &invalidFavoriteError{msg: fmt.Sprintf("title must be at most %d characters", maxFavoriteTextRunes)}
	}
	if utf8.RuneCountInString(subtitle) > maxFavoriteTextRunes {
		return &invalidFavoriteError{msg: fmt.Sprintf("subtitle must be at most %d characters", maxFavoriteTextRunes)}
	}
	return nil
}

func validateFavoriteImageURL(imageURL string) error {
	if imageURL == "" {
		return nil
	}
	if len(imageURL) > maxFavoriteImageURLBytes {
		return &invalidFavoriteError{msg: fmt.Sprintf("image_url must be at most %d bytes", maxFavoriteImageURLBytes)}
	}
	parsed, err := url.Parse(imageURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return &invalidFavoriteError{msg: "image_url must be an https URL"}
	}
	return nil
}

func hasTitleKey(kind domain.ResultKind, key string) bool {
	return key != "" && key != domain.FavoriteKey(kind, "", "")
}

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
	if err := validateFavoriteText(fav.Title, fav.Subtitle); err != nil {
		return err
	}
	if err := validateFavoriteImageURL(fav.ImageURL); err != nil {
		return err
	}
	fav.Key = domain.FavoriteKey(fav.Kind, fav.Title, fav.Subtitle)
	if !hasTitleKey(fav.Kind, fav.Key) {
		return &invalidFavoriteError{msg: "favorite needs a title"}
	}
	err := s.repo.Add(ctx, userId, fav)
	if errors.Is(err, ports.ErrFavoritesFull) {
		return favoritesFullError{}
	}
	if err != nil {
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

// favoritesLifter stably lifts the user's favorites to the front of the top
// ranked window. It replaces the favoritesRepo port that used to sit on the
// Service god object. A nil repository, the system user, or fewer than two
// results leave the ranking untouched.
type favoritesLifter struct {
	repo ports.FavoritesRepository
}

func newFavoritesLifter(repo ports.FavoritesRepository) *favoritesLifter {
	return &favoritesLifter{repo: repo}
}

func (s *favoritesLifter) lift(
	ctx context.Context,
	userId shared.UserId,
	ranked []domain.SearchResult,
) []domain.SearchResult {
	if s.repo == nil || len(ranked) < 2 {
		return ranked
	}
	if err := shared.GuardNotSystem(userId); err != nil {
		return ranked
	}
	favorites, err := s.repo.ListForUser(ctx, userId)
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

package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
)

var _ ports.FavoritesRepository = (*FavoritesRepo)(nil)

type FavoritesRepo struct {
	mu    sync.Mutex
	seq   int64
	byKey map[favoriteKey]storedFavorite
}

type favoriteKey struct {
	user ids.UserId
	kind domain.ResultKind
	key  string
}

type storedFavorite struct {
	favorite domain.Favorite
	order    int64
}

func (r *FavoritesRepo) Add(_ context.Context, userId ids.UserId, fav domain.Favorite) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := favoriteKey{userId, fav.Kind, fav.Key}
	if _, exists := r.byKey[k]; !exists && r.countLocked(userId) >= ports.MaxFavoritesPerUser {
		return fmt.Errorf("insert favorite: %w", domain.ErrFavoritesFull)
	}
	if r.byKey == nil {
		r.byKey = map[favoriteKey]storedFavorite{}
	}
	stored, exists := r.byKey[k]
	if !exists {
		r.seq++
		stored = storedFavorite{order: r.seq, favorite: domain.Favorite{Kind: fav.Kind, Key: fav.Key, CreatedAt: clock.Now()}}
	}
	stored.favorite.Title, stored.favorite.Subtitle, stored.favorite.ImageURL = fav.Title, fav.Subtitle, fav.ImageURL
	r.byKey[k] = stored
	return nil
}

func (r *FavoritesRepo) Remove(_ context.Context, userId ids.UserId, kind domain.ResultKind, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byKey, favoriteKey{userId, kind, key})
	return nil
}

func (r *FavoritesRepo) ListForUser(_ context.Context, userId ids.UserId) ([]domain.Favorite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ordered := make([]storedFavorite, 0)
	for k, s := range r.byKey {
		if k.user == userId {
			ordered = append(ordered, s)
		}
	}
	slices.SortFunc(ordered, func(a, b storedFavorite) int { return cmp.Compare(b.order, a.order) })
	out := make([]domain.Favorite, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.favorite)
	}
	return out, nil
}

func (r *FavoritesRepo) countLocked(userId ids.UserId) int {
	n := 0
	for k := range r.byKey {
		if k.user == userId {
			n++
		}
	}
	return n
}

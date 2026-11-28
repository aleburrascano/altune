package memory

import (
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"strings"
)

func (r *TrackRepo) ListFilteredForUser(ctx context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]*domain.Track, int, error) {
	tracks, total, err := r.ListForUser(ctx, userId, query.Limit, query.Offset)
	if err != nil || query.Search == "" {
		return tracks, total, err
	}
	needle := strings.ToLower(query.Search)
	var matched []*domain.Track
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title()), needle) ||
			strings.Contains(strings.ToLower(t.Artist()), needle) ||
			strings.Contains(strings.ToLower(t.Album()), needle) {
			matched = append(matched, t)
		}
	}
	return matched, len(matched), nil
}

func (r *TrackRepo) ListAlbumsForUser(_ context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	r.LastAlbumsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.AlbumGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId() != userId || t.Album() == "" {
			continue
		}
		artist := albumArtist(t)
		key := strings.ToLower(t.Album()) + "|||" + strings.ToLower(artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = newAlbumGroup(key, artist, t)
		order = append(order, key)
	}
	return inOrder(byKey, order), nil
}

func newAlbumGroup(key, artist string, t *domain.Track) *domain.AlbumGroup {
	return &domain.AlbumGroup{
		Key: key, Album: t.Album(), Artist: artist,
		ArtworkURL: t.ArtworkURL(), Year: t.Year(),
		TrackCount: 1, MostRecentAddedAt: t.AddedAt(),
	}
}

func albumArtist(t *domain.Track) string {
	if t.AlbumArtist() != nil {
		return *t.AlbumArtist()
	}
	return t.Artist()
}

func (r *TrackRepo) ListArtistsForUser(_ context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	r.LastArtistsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.ArtistGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId() != userId {
			continue
		}
		key := strings.ToLower(t.Artist())
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = newArtistGroup(key, t)
		order = append(order, key)
	}
	return inOrder(byKey, order), nil
}

func newArtistGroup(key string, t *domain.Track) *domain.ArtistGroup {
	return &domain.ArtistGroup{
		Key: key, Artist: t.Artist(), ArtworkURL: t.ArtworkURL(),
		TrackCount: 1, MostRecentAddedAt: t.AddedAt(),
	}
}

func inOrder[G any](byKey map[string]*G, order []string) []G {
	out := make([]G, 0, len(order))
	for _, key := range order {
		if group := byKey[key]; group != nil {
			out = append(out, *group)
		}
	}
	return out
}

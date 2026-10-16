package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"slices"
	"sync"
)

type GeniusSongCatalog struct {
	ArtistID    int
	ArtistFound bool
	Songs       []domain.UnreleasedSong
	MediaByID   map[int]bool
	DetailByID  map[int]domain.GeniusSongDetail
	Err         error
	MediaErr    error
	Gate        chan struct{}

	SearchedFor []string

	mu      sync.Mutex
	lookups []int
}

var _ ports.GeniusSongCatalog = (*GeniusSongCatalog)(nil)

func (g *GeniusSongCatalog) FindArtistID(ctx context.Context, artistName string, _ []string) (int, bool, error) {
	g.mu.Lock()
	g.SearchedFor = append(g.SearchedFor, artistName)
	g.mu.Unlock()
	if g.Gate != nil {
		select {
		case <-g.Gate:
		case <-ctx.Done():
			return 0, false, ctx.Err()
		}
	}
	return g.ArtistID, g.ArtistFound, g.Err
}

func (g *GeniusSongCatalog) ArtistSongs(_ context.Context, _, _ int) ([]domain.UnreleasedSong, error) {
	return append([]domain.UnreleasedSong(nil), g.Songs...), g.Err
}

func (g *GeniusSongCatalog) SongDetail(_ context.Context, songID int) (domain.GeniusSongDetail, error) {
	g.mu.Lock()
	g.lookups = append(g.lookups, songID)
	g.mu.Unlock()
	detail := g.DetailByID[songID]
	detail.HasMedia = detail.HasMedia || g.MediaByID[songID]
	return detail, g.MediaErr
}

func (g *GeniusSongCatalog) DetailLookups() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Sorted(slices.Values(g.lookups))
}

type ArtistCatalogTitles struct {
	Titles []string
	Err    error
}

var _ ports.ArtistCatalogTitles = (*ArtistCatalogTitles)(nil)

func (c *ArtistCatalogTitles) CatalogTitles(context.Context, domain.ProviderName, string, map[domain.ProviderName]string) ([]string, error) {
	return c.Titles, c.Err
}

package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
)

type GeniusSongCatalog struct {
	ArtistID    int
	ArtistFound bool
	Songs       []domain.UnreleasedSong
	MediaByID   map[int]bool
	Err         error
	MediaErr    error

	SearchedFor []string
}

var _ ports.GeniusSongCatalog = (*GeniusSongCatalog)(nil)

func (g *GeniusSongCatalog) FindArtistID(_ context.Context, artistName string, _ []string) (int, bool, error) {
	g.SearchedFor = append(g.SearchedFor, artistName)
	return g.ArtistID, g.ArtistFound, g.Err
}

func (g *GeniusSongCatalog) ArtistSongs(_ context.Context, _, _ int) ([]domain.UnreleasedSong, error) {
	return append([]domain.UnreleasedSong(nil), g.Songs...), g.Err
}

func (g *GeniusSongCatalog) HasMedia(_ context.Context, songID int) (bool, error) {
	return g.MediaByID[songID], g.MediaErr
}

type ArtistCatalogTitles struct {
	Titles []string
	Err    error
}

var _ ports.ArtistCatalogTitles = (*ArtistCatalogTitles)(nil)

func (c *ArtistCatalogTitles) CatalogTitles(context.Context, domain.ProviderName, string, map[domain.ProviderName]string) ([]string, error) {
	return c.Titles, c.Err
}

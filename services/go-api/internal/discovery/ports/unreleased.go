package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"time"
)

type GeniusSongCatalog interface {
	FindArtistID(ctx context.Context, artistName string, knownTitles []string) (int, bool, error)
	ArtistSongs(ctx context.Context, artistID, maxPages int) ([]domain.UnreleasedSong, error)
	HasMedia(ctx context.Context, songID int) (bool, error)
}

type ArtistCatalogTitles interface {
	CatalogTitles(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) ([]string, error)
}

type UnreleasedCache interface {
	Get(ctx context.Context, provider domain.ProviderName, externalID, nameKey string) ([]domain.UnreleasedSong, bool)
	Set(ctx context.Context, provider domain.ProviderName, externalID, nameKey string, songs []domain.UnreleasedSong, ttl time.Duration) error
}

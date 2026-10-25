package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"time"
)

type GeniusSongCatalog interface {
	FindArtistID(ctx context.Context, artistName string, knownTitles []string) (int, bool, error)
	ArtistSongs(ctx context.Context, artistID, maxPages int) ([]domain.UnreleasedSong, error)
	SongDetail(ctx context.Context, songID int) (domain.GeniusSongDetail, error)
}

var ErrPartialCatalog = errors.New("catalog titles are partial")

type ArtistCatalogTitles interface {
	CatalogTitles(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) ([]string, error)
}

type UnreleasedCache interface {
	Get(ctx context.Context, provider domain.ProviderName, externalID, nameKey string) ([]domain.UnreleasedSong, bool)
	Set(ctx context.Context, provider domain.ProviderName, externalID, nameKey string, songs []domain.UnreleasedSong, ttl time.Duration) error
}

var ErrTrackerPartial = errors.New("tracker sheet read in part")

type TrackerSongs interface {
	ArtistSongs(ctx context.Context, artistName string) ([]domain.UnreleasedSong, error)
}

type TrackerIndexCache interface {
	Get(ctx context.Context) (map[string]string, bool)
	Set(ctx context.Context, index map[string]string, ttl time.Duration) error
}

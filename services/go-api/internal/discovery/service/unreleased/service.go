package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"strings"
	"time"
)

type Config struct {
	MaxPages   int
	MaxDetails int
	TTL        time.Duration
	EmptyTTL   time.Duration
}

type Service struct {
	genius  ports.GeniusSongCatalog
	catalog ports.ArtistCatalogTitles
	cache   ports.UnreleasedCache
	cfg     Config
}

func NewService(genius ports.GeniusSongCatalog, catalog ports.ArtistCatalogTitles, cache ports.UnreleasedCache, cfg Config) *Service {
	return &Service{genius: genius, catalog: catalog, cache: cache, cfg: cfg}
}

func (s *Service) List(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	artistName = strings.TrimSpace(artistName)
	if artistName == "" {
		return []domain.UnreleasedSong{}, nil
	}
	nameKey := textnorm.NormalizeForIdentity(artistName)
	if s.cache != nil {
		if songs, ok := s.cache.Get(ctx, provider, externalID, nameKey); ok {
			return songs, nil
		}
	}
	songs, complete, err := s.build(ctx, provider, externalID, artistName, known)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if err := s.cache.Set(ctx, provider, externalID, nameKey, songs, s.cacheTTL(songs, complete)); err != nil {
			slog.WarnContext(ctx, "unreleased.cache_set_failed", "error", err)
		}
	}
	return songs, nil
}

func (s *Service) cacheTTL(songs []domain.UnreleasedSong, complete bool) time.Duration {
	if len(songs) == 0 || !complete {
		return s.cfg.EmptyTTL
	}
	return s.cfg.TTL
}

func (s *Service) build(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, bool, error) {
	titles, err := s.catalog.CatalogTitles(ctx, provider, externalID, known)
	if err != nil {
		return nil, false, err
	}
	artistID, found, err := s.genius.FindArtistID(ctx, artistName, titles)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return []domain.UnreleasedSong{}, true, nil
	}
	return s.unreleasedSongs(ctx, artistID, titles)
}

func (s *Service) unreleasedSongs(ctx context.Context, artistID int, catalogTitles []string) ([]domain.UnreleasedSong, bool, error) {
	songs, err := s.genius.ArtistSongs(ctx, artistID, s.cfg.MaxPages)
	if err != nil {
		return nil, false, err
	}
	unreleased := withoutCatalogMatches(songs, catalogTitles)
	complete, err := s.markMedia(ctx, unreleased)
	if err != nil {
		return nil, false, err
	}
	return unreleased, complete, nil
}

func withoutCatalogMatches(songs []domain.UnreleasedSong, catalogTitles []string) []domain.UnreleasedSong {
	released := make(map[string]struct{}, len(catalogTitles))
	for _, title := range catalogTitles {
		released[titleKey(title)] = struct{}{}
	}
	kept := make([]domain.UnreleasedSong, 0, len(songs))
	for _, song := range songs {
		if _, isReleased := released[titleKey(song.Title)]; !isReleased {
			kept = append(kept, song)
		}
	}
	return kept
}

func (s *Service) markMedia(ctx context.Context, songs []domain.UnreleasedSong) (complete bool, err error) {
	complete = true
	for i := range songs {
		if i >= s.cfg.MaxDetails {
			return complete, nil
		}
		hasMedia, err := s.genius.HasMedia(ctx, songs[i].GeniusID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		if err != nil {
			slog.WarnContext(ctx, "unreleased.media_lookup_failed", "genius_id", songs[i].GeniusID, "error", err)
			complete = false
			continue
		}
		songs[i].HasMedia = hasMedia
	}
	return complete, nil
}

package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/fanout"
	"altune/go-api/internal/shared/textnorm"
	"cmp"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

type Config struct {
	MaxPages          int
	MaxDetails        int
	DetailConcurrency int
	TTL               time.Duration
	EmptyTTL          time.Duration
	Wait              time.Duration
	JobTimeout        time.Duration
}

type Service struct {
	genius   ports.GeniusSongCatalog
	trackers ports.TrackerSongs
	catalog  ports.ArtistCatalogTitles
	cache    ports.UnreleasedCache
	store    ports.UnreleasedStore
	cfg      Config

	lifecycle context.Context
	jobs      *sync.WaitGroup
	mu        sync.Mutex
	inflight  map[string]*job
}

func NewService(genius ports.GeniusSongCatalog, catalog ports.ArtistCatalogTitles, cache ports.UnreleasedCache, cfg Config, opts ...Option) *Service {
	s := &Service{
		genius:    genius,
		catalog:   catalog,
		cache:     cache,
		cfg:       cfg,
		lifecycle: context.Background(),
		jobs:      &sync.WaitGroup{},
		inflight:  map[string]*job{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStore(store ports.UnreleasedStore) Option {
	return func(s *Service) { s.store = store }
}

func (s *Service) Rebuild(ctx context.Context, key ports.UnreleasedArtistKey, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	startedAt := time.Now()
	songs, complete, err := s.build(ctx, key.Provider, key.ExternalID, strings.TrimSpace(artistName), known)
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		if err := s.store.Write(ctx, key, songs, complete, startedAt); err != nil {
			slog.WarnContext(ctx, "unreleased.store_write_failed", "error", err)
		}
	}
	return songs, nil
}

func (s *Service) List(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	artistName = strings.TrimSpace(artistName)
	if artistName == "" {
		return []domain.UnreleasedSong{}, nil
	}
	if s.store != nil {
		return s.Rebuild(ctx, ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID}, artistName, known)
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
	catalogComplete := !errors.Is(err, ports.ErrPartialCatalog)
	if err != nil && catalogComplete {
		return nil, false, err
	}
	songs, complete, err := combineSources(s.geniusResult(ctx, artistName, titles), s.trackerResult(ctx, artistName, titles))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	complete = catalogComplete && complete
	if err == nil {
		logCoverage(ctx, provider, externalID, artistName, songs, complete)
	}
	return songs, complete, err
}

func (s *Service) unreleasedSongs(ctx context.Context, artistID int, catalogTitles []string) ([]domain.UnreleasedSong, bool, error) {
	songs, err := s.genius.ArtistSongs(ctx, artistID, s.cfg.MaxPages)
	if err != nil {
		return nil, false, err
	}
	candidates := withoutCatalogMatches(withoutTranslations(songs), catalogTitles)
	details, complete, err := s.lookUpDetails(ctx, candidates)
	if err != nil {
		return nil, false, err
	}
	return withoutReleasedEvidence(candidates, details, albumKeys(catalogTitles)), complete, nil
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

func albumKeys(catalogTitles []string) map[string]struct{} {
	keys := make(map[string]struct{}, len(catalogTitles))
	for _, title := range catalogTitles {
		keys[textnorm.NormalizeForIdentity(title)] = struct{}{}
	}
	return keys
}

type lookedUpDetail struct {
	detail domain.GeniusSongDetail
	found  bool
}

func (s *Service) lookUpDetails(ctx context.Context, songs []domain.UnreleasedSong) (details []lookedUpDetail, complete bool, err error) {
	details = make([]lookedUpDetail, len(songs))
	g := fanout.New(max(s.cfg.DetailConcurrency, 1))
	looked := max(min(len(songs), s.cfg.MaxDetails), 0)
	for i := range looked {
		g.Go(func() { details[i] = s.lookUpDetail(ctx, songs[i]) })
	}
	g.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	return details, !slices.ContainsFunc(details[:looked], func(d lookedUpDetail) bool { return !d.found }), nil
}

func (s *Service) lookUpDetail(ctx context.Context, song domain.UnreleasedSong) lookedUpDetail {
	detail, err := s.genius.SongDetail(ctx, song.GeniusID)
	if err != nil {
		slog.WarnContext(ctx, "unreleased.media_lookup_failed", "genius_id", song.GeniusID, "error", err)
		return lookedUpDetail{}
	}
	return lookedUpDetail{detail: detail, found: true}
}

func withoutReleasedEvidence(songs []domain.UnreleasedSong, details []lookedUpDetail, catalogAlbums map[string]struct{}) []domain.UnreleasedSong {
	kept := make([]domain.UnreleasedSong, 0, len(songs))
	for i, song := range songs {
		if details[i].found && isReleasedPerGenius(details[i].detail, catalogAlbums) {
			continue
		}
		if details[i].found {
			hasMedia := details[i].detail.HasMedia
			song.HasMedia = &hasMedia
			song.AudioLinks = details[i].detail.AudioLinks
			song.CoverURL = cmp.Or(song.CoverURL, details[i].detail.AlbumCoverURL)
		}
		kept = append(kept, song)
	}
	return kept
}

func isReleasedPerGenius(detail domain.GeniusSongDetail, catalogAlbums map[string]struct{}) bool {
	return detail.OnStreamingSite || isCatalogAlbum(detail.AlbumTitle, catalogAlbums)
}

func isCatalogAlbum(albumTitle string, catalogAlbums map[string]struct{}) bool {
	key := textnorm.NormalizeForIdentity(albumTitle)
	if key == "" || strings.HasPrefix(key, "unreleased") {
		return false
	}
	_, ok := catalogAlbums[key]
	return ok
}

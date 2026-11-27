package unreleased

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/textnorm"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

const CurrentBuildVersion = 8

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
	storage  Storage
	cfg      Config
	newTimer clock.TimerFunc
	now      clock.Func

	shutdown <-chan struct{}
	jobs     *background.Tracker
	mu       sync.Mutex
	inflight map[string]*job
}

var (
	ErrCollaboratorRequired = errors.New("unreleased service needs every collaborator, pass the null object to switch one off")
	ErrSourceNotConfigured  = errors.New("unreleased source not configured")
)

type NoCache struct{}

func (NoCache) Get(context.Context, domain.ProviderName, string, string) ([]domain.UnreleasedSong, bool) {
	return nil, false
}

func (NoCache) Set(context.Context, domain.ProviderName, string, string, []domain.UnreleasedSong, time.Duration) error {
	return nil
}

type NoGenius struct{}

func (NoGenius) FindArtistID(context.Context, string, []string) (int, bool, error) {
	return 0, false, ErrSourceNotConfigured
}

func (NoGenius) ArtistSongs(context.Context, int, int) ([]domain.UnreleasedSong, error) {
	return nil, ErrSourceNotConfigured
}

func (NoGenius) SongDetail(context.Context, int) (domain.GeniusSongDetail, error) {
	return domain.GeniusSongDetail{}, ErrSourceNotConfigured
}

type Wiring struct {
	Trackers ports.TrackerSongs
	Storage  Storage
	Shutdown <-chan struct{}
	Jobs     *background.Tracker
	Timer    clock.TimerFunc
	Now      clock.Func
}

func NewService(genius ports.GeniusSongCatalog, catalog ports.ArtistCatalogTitles, cfg Config, w Wiring) (*Service, error) {
	for name, missing := range map[string]bool{
		"genius":   genius == nil,
		"catalog":  catalog == nil,
		"trackers": w.Trackers == nil,
		"storage":  w.Storage == nil,
		"jobs":     w.Jobs == nil,
		"now":      w.Now == nil,
	} {
		if missing {
			return nil, fmt.Errorf("%w: %s is nil", ErrCollaboratorRequired, name)
		}
	}
	if cfg.Wait > 0 && w.Timer == nil {
		return nil, fmt.Errorf("%w: a Wait needs a timer", ErrCollaboratorRequired)
	}
	return &Service{
		genius:   genius,
		newTimer: w.Timer,
		now:      w.Now,
		trackers: w.Trackers,
		catalog:  catalog,
		storage:  w.Storage,
		cfg:      cfg,
		shutdown: w.Shutdown,
		jobs:     w.Jobs,
		inflight: map[string]*job{},
	}, nil
}

func (s *Service) Rebuild(ctx context.Context, key ports.UnreleasedArtistKey, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	startedAt := s.now()
	songs, complete, err := s.build(ctx, key.Provider, key.ExternalID, strings.TrimSpace(artistName), known)
	if err != nil {
		return nil, err
	}
	s.storage.persist(ctx, key, songs, complete, startedAt)
	return songs, nil
}

func (s *Service) List(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) ([]domain.UnreleasedSong, error) {
	artistName = strings.TrimSpace(artistName)
	if artistName == "" {
		return []domain.UnreleasedSong{}, nil
	}
	return s.storage.list(ctx, s, provider, externalID, artistName, known)
}

func (s *Service) cacheTTL(songs []domain.UnreleasedSong) time.Duration {
	if len(songs) == 0 {
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
	combined, err := combineSources(s.geniusResult(ctx, artistName, titles), s.trackerResult(ctx, artistName, titles))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	complete := catalogComplete && combined.complete
	if err == nil {
		logCoverage(ctx, provider, externalID, artistName, combined.songs, complete, combined.nonSongDropped)
	}
	return combined.songs, complete, err
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
	g := parallel.New("unreleased.detail_lookup_panic", max(s.cfg.DetailConcurrency, 1))
	looked := max(min(len(songs), s.cfg.MaxDetails), 0)
	for i := range looked {
		g.Go(ctx, func() { details[i] = s.lookUpDetail(ctx, songs[i]) })
	}
	panicked := g.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	complete = panicked == nil && !slices.ContainsFunc(details[:looked], func(d lookedUpDetail) bool { return !d.found })
	return details, complete, nil
}

func (s *Service) lookUpDetail(ctx context.Context, song domain.UnreleasedSong) lookedUpDetail {
	detail, err := s.genius.SongDetail(ctx, song.GeniusID)
	if err != nil {
		slog.WarnContext(ctx, "unreleased.media_lookup_failed", "genius_id", song.GeniusID, logging.KeyError, err)
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

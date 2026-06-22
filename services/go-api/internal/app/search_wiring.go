package app

import (
	"context"
	"net/http"
	"time"

	catalogPersistence "altune/go-api/internal/catalog/adapters/persistence"
	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/adapters/providers"
	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// BuildSearchService constructs the discovery search pipeline exactly as the API
// server wires it, so offline tooling (the library eval) exercises the same
// ranking the user sees. The single construction site keeps the eval from
// drifting away from production as wiring changes.
//
// eventStore is injected (not built here) so the server can share its telemetry
// store while the eval passes nil — eval searches are synthetic and must not
// pollute telemetry. redisClient may be nil (cache/vocab options are skipped).
func BuildSearchService(
	cfg *config.Config,
	pool *pgxpool.Pool,
	redisClient *goredis.Client,
	eventStore discoveryPorts.EventStore,
) *discoveryService.Service {
	var sharedMB *providers.MusicBrainzAdapter
	if cfg.HasMusicBrainz() {
		sharedMB = providers.NewMusicBrainzAdapter(
			&http.Client{Timeout: 10 * time.Second},
			cfg.MusicBrainzUserAgent,
		)
	}

	searchProviders := buildDiscoveryProviders(cfg, sharedMB)
	circuitBreaker := discoveryService.NewCircuitBreaker()
	historyRepo := discoveryPersistence.NewPgxSearchHistoryRepository(pool)

	deezerContent := providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second})
	trackRepo := catalogPersistence.NewPgxTrackRepository(pool)
	findRelatedSvc := discoveryService.NewFindRelatedService(trackRepo, deezerContent, deezerContent)

	opts := []discoveryService.Option{
		discoveryService.WithHistoryRepository(historyRepo),
		discoveryService.WithArtworkResolver(buildArtworkChain(cfg)),
		discoveryService.WithFindRelatedService(findRelatedSvc),
	}
	if redisClient != nil {
		opts = append(opts, discoveryService.WithArtworkCache(
			discoveryCacheAdapters.NewRedisArtworkCache(redisClient),
		))
		// The enrichment cache triples as the identity bridge (MB → provider id
		// graph that merge reads) and the MBID index (name → mbid memo that lets
		// search-card artwork attach an MBID to a non-MB result). Both detail-open
		// warmed, both cache-only — no extra MB call on the search path.
		enrichmentCache := discoveryCacheAdapters.NewRedisEnrichmentCache(redisClient)
		opts = append(opts, discoveryService.WithIdentityBridge(enrichmentCache))
		opts = append(opts, discoveryService.WithMBIDIndex(enrichmentCache))
	}
	if vocabStore := BuildVocabularyStore(redisClient); vocabStore != nil {
		opts = append(opts, discoveryService.WithVocabularyStore(vocabStore))
	}
	if eventStore != nil {
		opts = append(opts, discoveryService.WithEventStore(eventStore))
	}
	if sharedMB != nil {
		opts = append(opts, discoveryService.WithAlbumValidator(sharedMB))
	}

	return discoveryService.NewService(searchProviders, circuitBreaker, opts...)
}

// BuildConsensusProviders builds the multi-provider album fan-out used by the
// artist-content consensus AND the offline coverage signal B. One definition so
// the diagnostic measures the same provider set the app uses. Config-gated
// identically to the server wiring.
func BuildConsensusProviders(cfg *config.Config) []discoveryService.ConsensusProvider {
	var consensusProviders []discoveryService.ConsensusProvider

	if cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(&http.Client{Timeout: 10 * time.Second}, cfg.LastFMAPIKey)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "lastfm",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				return lfm.GetArtistAlbums(ctx, domain.ProviderLastFM, artistName)
			},
		})
	}
	if cfg.HasMusicBrainz() {
		mb := providers.NewMusicBrainzAdapter(&http.Client{Timeout: 10 * time.Second}, cfg.MusicBrainzUserAgent)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "musicbrainz",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				validated, err := mb.ValidateArtistAlbums(ctx, artistName, nil)
				if err != nil || validated == nil {
					return nil, err
				}
				return validated.Confirmed, nil
			},
		})
	}
	if cfg.HasDiscogs() {
		discogs := providers.NewDiscogsAdapter(&http.Client{Timeout: 10 * time.Second}, cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
			Name: "discogs",
			Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
				info, err := discogs.ResolveDiscogsArtist(ctx, artistName, nil)
				if err != nil || info == nil {
					return nil, err
				}
				releases, err := discogs.FetchArtistReleases(ctx, info.ID)
				if err != nil {
					return nil, err
				}
				results := make([]domain.SearchResult, 0, len(releases))
				for _, r := range releases {
					results = append(results, domain.SearchResult{
						Kind:  domain.ResultKindAlbum,
						Title: r.Title,
						Extras: map[string]any{
							"year":        r.Year,
							"record_type": r.Type,
						},
					})
				}
				return results, nil
			},
		})
	}

	itunes := providers.NewITunesAdapter(&http.Client{Timeout: 10 * time.Second})
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "itunes",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return itunes.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
		},
	})

	ytmusic := providers.NewYouTubeMusicAdapter()
	consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{
		Name: "ytmusic",
		Fetcher: func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
			return ytmusic.GetArtistAlbums(ctx, domain.ProviderYouTube, artistName)
		},
	})

	return consensusProviders
}

// buildArtworkChain assembles the artwork resolver chain: ID-based sources first
// (always correct for the entity), name-search fallbacks last.
func buildArtworkChain(cfg *config.Config) discoveryPorts.ArtworkResolver {
	var artworkResolvers []discoveryPorts.ArtworkResolver
	artworkResolvers = append(artworkResolvers,
		providers.NewCoverArtArchiveResolver(&http.Client{Timeout: 10 * time.Second}))
	if cfg.HasFanartTV() {
		artworkResolvers = append(artworkResolvers,
			providers.NewFanartTvArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		artworkResolvers = append(artworkResolvers,
			providers.NewGeniusArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.GeniusAccessToken))
	}
	artworkResolvers = append(artworkResolvers,
		providers.NewTheAudioDBAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewDeezerAdapter(&http.Client{Timeout: 10 * time.Second}),
		providers.NewITunesAdapter(&http.Client{Timeout: 10 * time.Second}),
	)
	if cfg.HasYouTube() {
		artworkResolvers = append(artworkResolvers,
			providers.NewYouTubeArtworkResolver(&http.Client{Timeout: 10 * time.Second}, cfg.YouTubeAPIKey))
	}
	// SoundCloud last: name-search fallback for the underground long tail no
	// ID-based source covers. nil fallback — artwork resolution never uses yt-dlp.
	artworkResolvers = append(artworkResolvers,
		providers.NewSoundCloudAPIAdapter(&http.Client{Timeout: 10 * time.Second}, nil))
	return providers.NewChainedArtworkResolver(artworkResolvers...)
}

// BuildVocabularyStore returns the Redis-backed vocabulary store, or nil when
// Redis is not configured.
func BuildVocabularyStore(redisClient *goredis.Client) discoveryPorts.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discoveryCacheAdapters.NewVocabularyStore(
		redisClient,
		discoveryService.NormalizeForMatch,
		discoveryCacheAdapters.WithMetaphone(discoveryService.MetaphoneKey),
	)
}

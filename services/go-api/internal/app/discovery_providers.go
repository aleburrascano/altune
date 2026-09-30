package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"net/http"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"

	goredis "github.com/redis/go-redis/v9"
)

func (a *App) buildDetailEnrichers(cf clientFactory) discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if lfmEnricher := buildLastFMAdapter(a.cfg, cf.discovery()); lfmEnricher != nil {
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discoveryCacheAdapters.NewRedisLastFmEnrichmentCache(a.redisClient, cacheSignalOption()),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		providers.NewDeezerAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient, cacheSignalOption()),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		providers.NewDeezerLyricsAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient, cacheSignalOption()),
	)

	return enrichers
}

type consensusAdder func(key discoveryDomain.ProviderKey, provider discoveryDomain.ProviderName, fetcher func(context.Context, string) ([]discoveryDomain.SearchResult, error))

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []discoveryService.ConsensusProvider {
	cf := newClientFactory(transport)
	var consensusProviders []discoveryService.ConsensusProvider
	add := func(key discoveryDomain.ProviderKey, provider discoveryDomain.ProviderName, fetcher func(context.Context, string) ([]discoveryDomain.SearchResult, error)) {
		consensusProviders = append(consensusProviders, discoveryService.ConsensusProvider{Name: string(key), Provider: provider, Fetcher: fetcher})
	}
	addCatalogConsensusProviders(cf, cfg, add)
	addStreamingConsensusProviders(cf, cfg, add)
	return consensusProviders
}

func addCatalogConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	if cfg.HasLastFM() {
		lfm := providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
		add(discoveryDomain.ProviderKeyLastFM, discoveryDomain.ProviderLastFM, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return lfm.GetArtistAlbums(ctx, discoveryDomain.ProviderLastFM, artistName)
		})
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		add(discoveryDomain.ProviderKeyMusicBrainz, discoveryDomain.ProviderMusicBrainz, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return mb.ListArtistDiscography(ctx, artistName)
		})
	}
	if cfg.HasDiscogs() {
		discogs := providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		add(discoveryDomain.ProviderKeyDiscogs, discoveryDomain.ProviderDiscogs, discogsConsensusFetcher(discogs))
	}
}

func addStreamingConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	add(discoveryDomain.ProviderKeyITunes, discoveryDomain.ProviderITunes, albumSearchFetcher(providers.NewITunesAdapter(cf.discovery())))
	if cfg.HasYouTubeMusic() {
		ytmusic := providers.NewYouTubeMusicAdapter(cf.roundTripper())
		add(discoveryDomain.ProviderKeyYTMusic, discoveryDomain.ProviderYouTube, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return ytmusic.GetArtistAlbums(ctx, discoveryDomain.ProviderYouTube, artistName)
		})
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		add(discoveryDomain.ProviderKeySoundCloud, discoveryDomain.ProviderSoundCloud, albumSearchFetcher(sc))
	}
}

func albumSearchFetcher(p interface {
	Search(ctx context.Context, query string, kinds map[discoveryDomain.ResultKind]bool) ([]discoveryDomain.SearchResult, error)
},
) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		return p.Search(ctx, artistName, map[discoveryDomain.ResultKind]bool{discoveryDomain.ResultKindAlbum: true})
	}
}

func discogsConsensusFetcher(discogs *providers.DiscogsAdapter) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		info, err := discogs.ResolveDiscogsArtist(ctx, artistName, nil)
		if err != nil || info == nil {
			return nil, err
		}
		releases, err := discogs.FetchArtistReleases(ctx, info.ID)
		if err != nil {
			return nil, err
		}
		return discogsReleasesToSearchResults(releases), nil
	}
}

func discogsReleasesToSearchResults(releases []discoveryPorts.DiscogsRelease) []discoveryDomain.SearchResult {
	results := make([]discoveryDomain.SearchResult, 0, len(releases))
	for _, r := range releases {
		results = append(results, discoveryDomain.SearchResult{
			Kind:       discoveryDomain.ResultKindAlbum,
			Title:      r.Title,
			RecordType: discoveryDomain.RecordType(r.Type),
			Extras: map[string]any{
				"year": r.Year,
			},
		})
	}
	return results
}

func BuildArtworkChain(cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	return buildArtworkChain(newClientFactory(nil), cfg)
}

func buildArtworkChain(cf clientFactory, cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	coverArtArchive := providers.NewCoverArtArchiveResolver(cf.discovery())
	artworkResolvers := []discoveryPorts.ArtworkResolver{
		coverArtArchive,
		providers.NewCoverArtArchiveIdentityResolver(coverArtArchive),
	}
	artworkResolvers = append(artworkResolvers, keyedArtworkResolvers(cf, cfg)...)
	artworkResolvers = append(artworkResolvers, openArtworkResolvers(cf, cfg)...)
	return providers.NewChainedArtworkResolver(artworkResolvers...)
}

func keyedArtworkResolvers(cf clientFactory, cfg *config.Config) []discoveryPorts.ArtworkResolver {
	var resolvers []discoveryPorts.ArtworkResolver
	if cfg.HasSpotify() {
		resolvers = append(resolvers, providers.NewSpotifyArtworkResolver(cf.discovery()))
	}
	if cfg.HasDiscogs() {
		resolvers = append(resolvers,
			providers.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent))
	}
	if cfg.HasFanartTV() {
		resolvers = append(resolvers,
			providers.NewFanartTvArtworkResolver(cf.discovery(), cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		resolvers = append(resolvers,
			providers.NewGeniusArtworkResolver(cf.discovery(), cfg.GeniusAccessToken))
	}
	return resolvers
}

func openArtworkResolvers(cf clientFactory, cfg *config.Config) []discoveryPorts.ArtworkResolver {
	resolvers := []discoveryPorts.ArtworkResolver{
		providers.NewTheAudioDBAdapter(cf.discovery()),
		providers.NewDeezerAdapter(cf.discovery()),
		providers.NewITunesAdapter(cf.discovery()),
	}
	if cfg.HasYouTubeMusic() {
		resolvers = append(resolvers, providers.NewYouTubeMusicArtworkResolver(cf.roundTripper()))
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		resolvers = append(resolvers, sc)
	}
	return resolvers
}

func BuildVocabularyStore(redisClient *goredis.Client) discoveryPorts.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discoveryCacheAdapters.NewVocabularyStore(
		redisClient,
		textnorm.NormalizeForMatch,
		discoveryCacheAdapters.WithMetaphone(phonetics.MetaphoneKey),
		discoveryCacheAdapters.WithVocabSignal(cacheSignal()),
	)
}

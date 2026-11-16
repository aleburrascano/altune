package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"net/http"

	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryEnrich "altune/go-api/internal/discovery/service/enrich"

	goredis "github.com/redis/go-redis/v9"
)

func (a *App) buildDetailEnrichers(cf clientFactory) discoveryHandler.DetailEnrichers {
	var enrichers discoveryHandler.DetailEnrichers

	if lfmEnricher := buildLastFMAdapter(a.cfg, cf.discovery()); lfmEnricher != nil {
		enrichers.LastFm = discoveryEnrich.NewLastFmEnrichmentService(
			lfmEnricher,
			discovery.NewRedisLastFmEnrichmentCache(a.redisClient, cacheSignalOption()),
		)
	}

	enrichers.Deezer = discoveryEnrich.NewDeezerEnrichmentService(
		discovery.NewDeezerAdapter(cf.discovery()),
		discovery.NewRedisDeezerEnrichmentCache(a.redisClient, cacheSignalOption()),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		discovery.NewDeezerLyricsAdapter(cf.discovery()),
		discovery.NewRedisDeezerLyricsCache(a.redisClient, cacheSignalOption()),
	)

	return enrichers
}

type consensusAdder func(key discoveryDomain.ProviderKey, provider discoveryDomain.ProviderName, fetcher func(context.Context, string) ([]discoveryDomain.SearchResult, error))

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []providercall.ConsensusProvider {
	cf := newClientFactory(transport)
	var consensusProviders []providercall.ConsensusProvider
	add := func(key discoveryDomain.ProviderKey, provider discoveryDomain.ProviderName, fetcher func(context.Context, string) ([]discoveryDomain.SearchResult, error)) {
		consensusProviders = append(consensusProviders, providercall.ConsensusProvider{Name: string(key), Provider: provider, Fetcher: fetcher})
	}
	addCatalogConsensusProviders(cf, cfg, add)
	addStreamingConsensusProviders(cf, cfg, add)
	return consensusProviders
}

func addCatalogConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	if cfg.HasLastFM() {
		lfm := discovery.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
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
		discogsAdapter := discovery.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		add(discoveryDomain.ProviderKeyDiscogs, discoveryDomain.ProviderDiscogs, discogsConsensusFetcher(discogsAdapter))
	}
}

func addStreamingConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	add(discoveryDomain.ProviderKeyITunes, discoveryDomain.ProviderITunes, albumSearchFetcher(discovery.NewITunesAdapter(cf.discovery())))
	if cfg.HasYouTubeMusic() {
		ytmusicAdapter := discovery.NewYouTubeMusicAdapter(cf.roundTripper())
		add(discoveryDomain.ProviderKeyYTMusic, discoveryDomain.ProviderYouTube, func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
			return ytmusicAdapter.GetArtistAlbums(ctx, discoveryDomain.ProviderYouTube, artistName)
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

func discogsConsensusFetcher(adapter *discovery.DiscogsAdapter) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		info, err := adapter.ResolveDiscogsArtist(ctx, artistName, nil)
		if err != nil || info == nil {
			return nil, err
		}
		releases, err := adapter.FetchArtistReleases(ctx, info.ID)
		if err != nil {
			return nil, err
		}
		return discovery.DiscogsReleasesToSearchResults(releases), nil
	}
}

func buildArtworkChain(cf clientFactory, cfg *config.Config) discovery.TaggingArtworkResolver {
	coverArtArchive := discovery.NewCoverArtArchiveResolver(cf.discovery())
	artworkResolvers := []discovery.ArtworkResolver{
		coverArtArchive,
		discovery.NewCoverArtArchiveIdentityResolver(coverArtArchive),
	}
	artworkResolvers = append(artworkResolvers, keyedArtworkResolvers(cf, cfg)...)
	artworkResolvers = append(artworkResolvers, openArtworkResolvers(cf, cfg)...)
	return discovery.NewChainedArtworkResolver(artworkResolvers...)
}

func keyedArtworkResolvers(cf clientFactory, cfg *config.Config) []discovery.ArtworkResolver {
	var resolvers []discovery.ArtworkResolver
	if cfg.HasSpotify() {
		resolvers = append(resolvers, discovery.NewSpotifyArtworkResolver(cf.discovery()))
	}
	if cfg.HasDiscogs() {
		resolvers = append(resolvers, discovery.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent))
	}
	if cfg.HasFanartTV() {
		resolvers = append(resolvers, discovery.NewFanartTvArtworkResolver(cf.discovery(), cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		resolvers = append(resolvers, discovery.NewGeniusArtworkResolver(cf.discovery(), cfg.GeniusAccessToken))
	}
	if cfg.HasTheAudioDB() {
		resolvers = append(resolvers, discovery.NewTheAudioDBAdapter(cf.discovery(), discovery.WithTheAudioDBAPIKey(cfg.TheAudioDBAPIKey)))
	}
	return resolvers
}

func openArtworkResolvers(cf clientFactory, cfg *config.Config) []discovery.ArtworkResolver {
	resolvers := []discovery.ArtworkResolver{
		discovery.NewDeezerAdapter(cf.discovery()),
		discovery.NewITunesAdapter(cf.discovery()),
	}
	if cfg.HasYouTubeMusic() {
		resolvers = append(resolvers, discovery.NewYouTubeMusicArtworkResolver(cf.roundTripper()))
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		resolvers = append(resolvers, sc)
	}
	return resolvers
}

func BuildVocabularyStore(redisClient *goredis.Client) discovery.VocabularyStore {
	if redisClient == nil {
		return nil
	}
	return discovery.NewVocabularyStore(
		redisClient,
		textnorm.NormalizeForMatch,
		discovery.WithMetaphone(phonetics.MetaphoneKey),
		discovery.WithVocabSignal(cacheSignal()),
	)
}

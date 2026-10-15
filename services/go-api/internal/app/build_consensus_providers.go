package app

import (
	"altune/go-api/internal/discovery/adapters/providers/artworkchain"
	"altune/go-api/internal/discovery/adapters/providers/coverartarchive"
	"altune/go-api/internal/discovery/adapters/providers/deezer"
	"altune/go-api/internal/discovery/adapters/providers/discogs"
	"altune/go-api/internal/discovery/adapters/providers/fanarttv"
	"altune/go-api/internal/discovery/adapters/providers/genius"
	"altune/go-api/internal/discovery/adapters/providers/itunes"
	"altune/go-api/internal/discovery/adapters/providers/lastfm"
	"altune/go-api/internal/discovery/adapters/providers/spotify"
	"altune/go-api/internal/discovery/adapters/providers/theaudiodb"
	"altune/go-api/internal/discovery/adapters/providers/ytmusic"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"net/http"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
	discoveryHandler "altune/go-api/internal/discovery/adapters/handler"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
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
		deezer.NewDeezerAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerEnrichmentCache(a.redisClient, cacheSignalOption()),
	)

	enrichers.Lyrics = discoveryEnrich.NewLyricsService(
		deezer.NewDeezerLyricsAdapter(cf.discovery()),
		discoveryCacheAdapters.NewRedisDeezerLyricsCache(a.redisClient, cacheSignalOption()),
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
		lfm := lastfm.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
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
		discogsAdapter := discogs.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		add(discoveryDomain.ProviderKeyDiscogs, discoveryDomain.ProviderDiscogs, discogsConsensusFetcher(discogsAdapter))
	}
}

func addStreamingConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	add(discoveryDomain.ProviderKeyITunes, discoveryDomain.ProviderITunes, albumSearchFetcher(itunes.NewITunesAdapter(cf.discovery())))
	if cfg.HasYouTubeMusic() {
		ytmusicAdapter := ytmusic.NewYouTubeMusicAdapter(cf.roundTripper())
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

func discogsConsensusFetcher(adapter *discogs.DiscogsAdapter) func(context.Context, string) ([]discoveryDomain.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discoveryDomain.SearchResult, error) {
		info, err := adapter.ResolveDiscogsArtist(ctx, artistName, nil)
		if err != nil || info == nil {
			return nil, err
		}
		releases, err := adapter.FetchArtistReleases(ctx, info.ID)
		if err != nil {
			return nil, err
		}
		return discogs.DiscogsReleasesToSearchResults(releases), nil
	}
}

func buildArtworkChain(cf clientFactory, cfg *config.Config) discoveryPorts.TaggingArtworkResolver {
	coverArtArchive := coverartarchive.NewCoverArtArchiveResolver(cf.discovery())
	artworkResolvers := []discoveryPorts.ArtworkResolver{
		coverArtArchive,
		coverartarchive.NewCoverArtArchiveIdentityResolver(coverArtArchive),
	}
	artworkResolvers = append(artworkResolvers, keyedArtworkResolvers(cf, cfg)...)
	artworkResolvers = append(artworkResolvers, openArtworkResolvers(cf, cfg)...)
	return artworkchain.NewChainedArtworkResolver(artworkResolvers...)
}

func keyedArtworkResolvers(cf clientFactory, cfg *config.Config) []discoveryPorts.ArtworkResolver {
	var resolvers []discoveryPorts.ArtworkResolver
	if cfg.HasSpotify() {
		resolvers = append(resolvers, spotify.NewSpotifyArtworkResolver(cf.discovery()))
	}
	if cfg.HasDiscogs() {
		resolvers = append(resolvers,
			discogs.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent))
	}
	if cfg.HasFanartTV() {
		resolvers = append(resolvers,
			fanarttv.NewFanartTvArtworkResolver(cf.discovery(), cfg.FanartTVAPIKey))
	}
	if cfg.HasGenius() {
		resolvers = append(resolvers,
			genius.NewGeniusArtworkResolver(cf.discovery(), cfg.GeniusAccessToken))
	}
	if cfg.HasTheAudioDB() {
		resolvers = append(resolvers,
			theaudiodb.New(cf.discovery(), theaudiodb.WithAPIKey(cfg.TheAudioDBAPIKey)))
	}
	return resolvers
}

func openArtworkResolvers(cf clientFactory, cfg *config.Config) []discoveryPorts.ArtworkResolver {
	resolvers := []discoveryPorts.ArtworkResolver{
		deezer.NewDeezerAdapter(cf.discovery()),
		itunes.NewITunesAdapter(cf.discovery()),
	}
	if cfg.HasYouTubeMusic() {
		resolvers = append(resolvers, ytmusic.NewYouTubeMusicArtworkResolver(cf.roundTripper()))
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

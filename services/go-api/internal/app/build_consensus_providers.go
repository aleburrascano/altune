package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"net/http"

	goredis "github.com/redis/go-redis/v9"
)

type consensusAdder func(key discovery.ProviderKey, provider discovery.ProviderName, fetcher func(context.Context, string) ([]discovery.SearchResult, error))

func BuildConsensusProviders(cfg *config.Config, transport http.RoundTripper) []discovery.ConsensusProvider {
	cf := newClientFactory(transport)
	var consensusProviders []discovery.ConsensusProvider
	add := func(key discovery.ProviderKey, provider discovery.ProviderName, fetcher func(context.Context, string) ([]discovery.SearchResult, error)) {
		consensusProviders = append(consensusProviders, discovery.ConsensusProvider{Name: string(key), Provider: provider, Fetcher: fetcher})
	}
	addCatalogConsensusProviders(cf, cfg, add)
	addStreamingConsensusProviders(cf, cfg, add)
	return consensusProviders
}

func addCatalogConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	if cfg.HasLastFM() {
		lfm := discovery.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
		add(discovery.ProviderKeyLastFM, discovery.ProviderLastFM, func(ctx context.Context, artistName string) ([]discovery.SearchResult, error) {
			return lfm.GetArtistAlbums(ctx, discovery.ProviderLastFM, artistName)
		})
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		add(discovery.ProviderKeyMusicBrainz, discovery.ProviderMusicBrainz, func(ctx context.Context, artistName string) ([]discovery.SearchResult, error) {
			return mb.ListArtistDiscography(ctx, artistName)
		})
	}
	if cfg.HasDiscogs() {
		discogsAdapter := discovery.NewDiscogsAdapter(cf.discovery(), cfg.DiscogsToken, cfg.MusicBrainzUserAgent)
		add(discovery.ProviderKeyDiscogs, discovery.ProviderDiscogs, discogsConsensusFetcher(discogsAdapter))
	}
}

func addStreamingConsensusProviders(cf clientFactory, cfg *config.Config, add consensusAdder) {
	add(discovery.ProviderKeyITunes, discovery.ProviderITunes, albumSearchFetcher(discovery.NewITunesAdapter(cf.discovery())))
	if cfg.HasYouTubeMusic() {
		ytmusicAdapter := discovery.NewYouTubeMusicAdapter(cf.roundTripper())
		add(discovery.ProviderKeyYTMusic, discovery.ProviderYouTube, func(ctx context.Context, artistName string) ([]discovery.SearchResult, error) {
			return ytmusicAdapter.GetArtistAlbums(ctx, discovery.ProviderYouTube, artistName)
		})
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		add(discovery.ProviderKeySoundCloud, discovery.ProviderSoundCloud, albumSearchFetcher(sc))
	}
}

func albumSearchFetcher(p interface {
	Search(ctx context.Context, query string, kinds map[discovery.ResultKind]bool) ([]discovery.SearchResult, error)
},
) func(context.Context, string) ([]discovery.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discovery.SearchResult, error) {
		return p.Search(ctx, artistName, map[discovery.ResultKind]bool{discovery.ResultKindAlbum: true})
	}
}

func discogsConsensusFetcher(adapter *discovery.DiscogsAdapter) func(context.Context, string) ([]discovery.SearchResult, error) {
	return func(ctx context.Context, artistName string) ([]discovery.SearchResult, error) {
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

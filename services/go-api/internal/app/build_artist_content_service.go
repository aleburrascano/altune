package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"net/http"
)

func buildArtistContentProviders(
	cf clientFactory,
	cfg *config.Config,
) map[discovery.ProviderName]discovery.ArtistContentProvider {
	artistProviders := map[discovery.ProviderName]discovery.ArtistContentProvider{
		discovery.ProviderDeezer: discovery.NewDeezerAdapter(cf.discovery()),
	}
	if am := buildAppleMusicAdapter(cf, cfg); am != nil {
		artistProviders[discovery.ProviderAppleMusic] = am
	}
	if sp := buildSpotifyAdapter(cf, cfg); sp != nil {
		artistProviders[discovery.ProviderSpotify] = sp
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		artistProviders[discovery.ProviderSoundCloud] = sc
	}
	if lfm := buildLastFMAdapter(cfg, cf.discovery()); lfm != nil {
		artistProviders[discovery.ProviderLastFM] = lfm
	}
	return artistProviders
}

func BuildArtistContentService(
	cfg *config.Config,
	transport http.RoundTripper,
	store discovery.IdentityStore,
) *discovery.GetArtistContentService {
	cf := newClientFactory(transport)

	artistProviders := buildArtistContentProviders(cf, cfg)

	opts := []discovery.ArtistContentOption{
		discovery.WithContentIdentityStore(store),
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		opts = append(opts, discovery.WithMBAnchor(mb))
	}
	return discovery.NewGetArtistContentService(artistProviders, opts...)
}

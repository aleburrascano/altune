package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared/config"
	"net/http"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

func buildArtistContentProviders(
	cf clientFactory,
	cfg *config.Config,
) map[discoveryDomain.ProviderName]discoveryPorts.ArtistContentProvider {
	artistProviders := map[discoveryDomain.ProviderName]discoveryPorts.ArtistContentProvider{
		discoveryDomain.ProviderDeezer: providers.NewDeezerAdapter(cf.discovery()),
	}
	if am := buildAppleMusicAdapter(cf, cfg); am != nil {
		artistProviders[discoveryDomain.ProviderAppleMusic] = am
	}
	if sp := buildSpotifyAdapter(cf, cfg); sp != nil {
		artistProviders[discoveryDomain.ProviderSpotify] = sp
	}
	if sc := buildSoundCloudAdapter(cf, cfg); sc != nil {
		artistProviders[discoveryDomain.ProviderSoundCloud] = sc
	}
	if lfm := buildLastFMAdapter(cfg, cf.discovery()); lfm != nil {
		artistProviders[discoveryDomain.ProviderLastFM] = lfm
	}
	return artistProviders
}

func BuildArtistContentService(
	cfg *config.Config,
	transport http.RoundTripper,
	store discoveryPorts.IdentityStore,
) *discoveryService.GetArtistContentService {
	cf := newClientFactory(transport)

	artistProviders := buildArtistContentProviders(cf, cfg)

	opts := []discoveryService.ArtistContentOption{
		discoveryService.WithContentIdentityStore(store),
	}
	if mb := buildMusicBrainzAdapter(cf, cfg); mb != nil {
		opts = append(opts, discoveryService.WithMBAnchor(mb))
	}
	return discoveryService.NewGetArtistContentService(artistProviders, opts...)
}

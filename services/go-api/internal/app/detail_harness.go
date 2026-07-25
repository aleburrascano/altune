package app

import (
	"net/http"

	"altune/go-api/internal/discovery/adapters/providers"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
)

func BuildArtistContentService(
	cfg *config.Config,
	transport http.RoundTripper,
	store discoveryPorts.IdentityStore,
) *discoveryService.GetArtistContentService {
	cf := clientFactory{transport: transport}

	artistProviders := map[discoveryDomain.ProviderName]discoveryPorts.ArtistContentProvider{
		discoveryDomain.ProviderDeezer:     providers.NewDeezerAdapter(cf.discovery()),
		discoveryDomain.ProviderAppleMusic: providers.NewAppleMusicAdapter(cf.discovery()),
		discoveryDomain.ProviderSpotify:    providers.NewSpotifyAdapter(cf.discovery()),
		discoveryDomain.ProviderSoundCloud: providers.NewSoundCloudAPIAdapter(cf.discovery(), nil),
	}
	if cfg.HasLastFM() {
		artistProviders[discoveryDomain.ProviderLastFM] = providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
	}

	opts := []discoveryService.ArtistContentOption{
		discoveryService.WithContentIdentityStore(store),
	}
	if cfg.HasMusicBrainz() {
		mb := providers.NewMusicBrainzAdapter(cf.discovery(), cfg.MusicBrainzUserAgent)
		opts = append(opts, discoveryService.WithMBAnchor(mb))
	}
	return discoveryService.NewGetArtistContentService(artistProviders, opts...)
}

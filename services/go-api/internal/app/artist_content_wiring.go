package app

import (
	"altune/go-api/internal/discovery/adapters/providers"
	"altune/go-api/internal/shared/config"
	"net/http"

	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

// buildArtistContentProviders builds the canonical artist-content-provider map
// and is the single source of truth for it: Deezer, Apple Music, Spotify and
// SoundCloud (each behind its kill switch), plus Last.fm when configured.
// Callers that need a narrower set must derive it explicitly from this result.
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
	if cfg.HasLastFM() {
		artistProviders[discoveryDomain.ProviderLastFM] = providers.NewLastFmAdapter(cf.discovery(), cfg.LastFMAPIKey)
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

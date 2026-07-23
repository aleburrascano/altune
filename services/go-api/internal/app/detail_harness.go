package app

import (
	"net/http"

	"altune/go-api/internal/discovery/adapters/providers"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
)

// BuildArtistContentService constructs the detail/discography service the way
// wireDiscoveryContent does for production — the artist-content fan-out (Deezer,
// Apple Music, Spotify, SoundCloud, + Last.fm when configured) plus the
// MusicBrainz identity-verification anchor — but over a caller-supplied identity
// store. The discoveryeval `detail` harness passes a seeded in-memory store so it
// can feed deliberately fractured identities (a wrong streaming edge fusing two
// same-name artists) and assert the read-time guards clean them.
//
// AIDEV-NOTE: this mirrors wireDiscoveryContent's artistProviders map — the two
// must stay in sync when a content provider is added/removed. It is a harness
// seam, not a production wiring path; production still goes through wireDiscovery.
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

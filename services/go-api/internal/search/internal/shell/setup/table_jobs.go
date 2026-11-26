package setup

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/providers/artworkfallback"
	"altune/go-api/internal/search/internal/core/ports"
)

var artworkAskingOrder = []rowName{
	rowCoverArtArchive, rowSpotify, rowDiscogs, rowFanartTV, rowGenius, rowTheAudioDB,
	rowDeezer, rowITunes, rowYouTube, rowSoundCloud,
}

func (t *providerTable) searchProviders() []ports.SearchProvider {
	var searchers []ports.SearchProvider
	for _, p := range t.providers {
		if p.search != nil {
			searchers = append(searchers, p.search)
		}
	}
	return searchers
}

func (t *providerTable) artistProviders() map[domain.ProviderName]ports.ArtistContentProvider {
	artists := map[domain.ProviderName]ports.ArtistContentProvider{}
	for _, p := range t.providers {
		if p.artist != nil {
			artists[p.id] = p.artist
		}
	}
	return artists
}

func (t *providerTable) albumProviders() map[domain.ProviderName]ports.AlbumContentProvider {
	albums := map[domain.ProviderName]ports.AlbumContentProvider{}
	for _, p := range t.providers {
		if p.album != nil {
			albums[p.id] = p.album
		}
	}
	return albums
}

func (t *providerTable) relatedProviders() map[string]ports.RelatedTracksProvider {
	related := map[string]ports.RelatedTracksProvider{}
	for _, p := range t.providers {
		if p.related != nil {
			related[p.id.String()] = p.related
		}
	}
	return related
}

func (t *providerTable) chartProviders() []ports.ChartProvider {
	var charts []ports.ChartProvider
	for _, p := range t.providers {
		if p.charts != nil {
			charts = append(charts, p.charts)
		}
	}
	return charts
}

func (t *providerTable) artworkChain() *artworkfallback.ChainedArtworkLookup {
	var resolvers []ports.ArtworkLookup
	for _, name := range artworkAskingOrder {
		resolvers = append(resolvers, t.named(name).artwork...)
	}
	return artworkfallback.NewChainedArtworkLookup(resolvers...)
}

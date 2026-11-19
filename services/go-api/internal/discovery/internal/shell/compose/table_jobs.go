package compose

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/providers/artworkchain"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/providercall"
)

var artworkAskingOrder = []rowName{
	rowCoverArtArchive, rowSpotify, rowDiscogs, rowFanartTV, rowGenius, rowTheAudioDB,
	rowDeezer, rowITunes, rowYouTube, rowSoundCloud,
}

var consensusAskingOrder = []rowName{rowMusicBrainz, rowDiscogs, rowITunes, rowYouTube, rowSoundCloud}

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

func (t *providerTable) consensusProviders() []providercall.ConsensusProvider {
	var consensus []providercall.ConsensusProvider
	for _, name := range consensusAskingOrder {
		if p := t.named(name); p.consensus.Fetcher != nil {
			consensus = append(consensus, p.consensus)
		}
	}
	return consensus
}

func (t *providerTable) artworkChain() *artworkchain.ChainedArtworkResolver {
	var resolvers []ports.ArtworkResolver
	for _, name := range artworkAskingOrder {
		resolvers = append(resolvers, t.named(name).artwork...)
	}
	return artworkchain.NewChainedArtworkResolver(resolvers...)
}

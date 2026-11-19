package compose

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/providers/discogs"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
)

type discographyFetch func(ctx context.Context, artistName string) ([]domain.SearchResult, error)

type artistAlbumsLister interface {
	GetArtistAlbums(ctx context.Context, provider domain.ProviderName, artistName string) ([]domain.SearchResult, error)
}

type albumSearcher interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

func newConsensus(key domain.ProviderKey, provider domain.ProviderName, fetch discographyFetch) providercall.ConsensusProvider {
	return providercall.ConsensusProvider{Name: key.String(), Provider: provider, Fetcher: fetch}
}

func artistAlbumsConsensus(key domain.ProviderKey, provider domain.ProviderName, lister artistAlbumsLister) providercall.ConsensusProvider {
	return newConsensus(key, provider, func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
		return lister.GetArtistAlbums(ctx, provider, artistName)
	})
}

func albumSearchConsensus(key domain.ProviderKey, provider domain.ProviderName, searcher albumSearcher) providercall.ConsensusProvider {
	return newConsensus(key, provider, func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
		return searcher.Search(ctx, artistName, map[domain.ResultKind]bool{domain.ResultKindAlbum: true})
	})
}

func discogsReleases(adapter *discogs.DiscogsAdapter) discographyFetch {
	return func(ctx context.Context, artistName string) ([]domain.SearchResult, error) {
		artist, err := adapter.ResolveDiscogsArtist(ctx, artistName, nil)
		if err != nil || artist == nil {
			return nil, err
		}
		releases, err := adapter.FetchArtistReleases(ctx, artist.ID)
		if err != nil {
			return nil, err
		}
		return discogs.DiscogsReleasesToSearchResults(releases), nil
	}
}

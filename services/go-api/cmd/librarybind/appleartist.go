package main

import (
	"altune/go-api/internal/catalog/domain"
	discovery "altune/go-api/internal/discovery/domain"
	"context"

	"golang.org/x/time/rate"
)

type appleArtistAPI interface {
	Search(ctx context.Context, query string, kinds map[discovery.ResultKind]bool) ([]discovery.SearchResult, error)
	GetArtistAlbums(ctx context.Context, provider discovery.ProviderName, artistID string) ([]discovery.SearchResult, error)
	GetArtistTopTracks(ctx context.Context, provider discovery.ProviderName, artistID string) ([]discovery.SearchResult, error)
}

type appleArtistSource struct {
	api     appleArtistAPI
	limiter *rate.Limiter
}

func (appleArtistSource) Name() source { return sourceApple }

func appleCall[T any](ctx context.Context, limiter *rate.Limiter, call func() (T, error)) (T, error) {
	var none T
	if err := limiter.Wait(ctx); err != nil {
		return none, err
	}
	results, err := call()
	if err != nil {
		return none, appleFailure(err)
	}
	return results, nil
}

func (a appleArtistSource) search(ctx context.Context, query string, kind discovery.ResultKind) ([]discovery.SearchResult, error) {
	return appleCall(ctx, a.limiter, func() ([]discovery.SearchResult, error) {
		return a.api.Search(ctx, query, map[discovery.ResultKind]bool{kind: true})
	})
}

func (a appleArtistSource) FindArtists(ctx context.Context, name string) ([]providerArtist, error) {
	results, err := a.search(ctx, name, discovery.ResultKindArtist)
	if err != nil {
		return nil, err
	}
	var artists []providerArtist
	for _, result := range results {
		if result.Kind == discovery.ResultKindArtist && len(result.Sources) > 0 {
			artists = append(artists, providerArtist{ID: result.Sources[0].ExternalID, Name: result.Title})
		}
	}
	return artists, nil
}

func (a appleArtistSource) Catalog(ctx context.Context, artist providerArtist) (catalogView, error) {
	albums, err := appleCall(ctx, a.limiter, func() ([]discovery.SearchResult, error) {
		return a.api.GetArtistAlbums(ctx, discovery.ProviderAppleMusic, artist.ID)
	})
	if err != nil {
		return catalogView{}, err
	}
	top, err := appleCall(ctx, a.limiter, func() ([]discovery.SearchResult, error) {
		return a.api.GetArtistTopTracks(ctx, discovery.ProviderAppleMusic, artist.ID)
	})
	if err != nil {
		return catalogView{}, err
	}
	return catalogView{Albums: appleTitles(albums), Titles: appleTitles(top)}, nil
}

func appleTitles(results []discovery.SearchResult) []string {
	titles := make([]string, 0, len(results))
	for _, result := range results {
		titles = append(titles, result.Title)
	}
	return titles
}

func (a appleArtistSource) Songs(ctx context.Context, _ providerArtist, view catalogView, _ libraryTrack, keys domain.SearchKeys) ([]candidate, error) {
	results, err := a.search(ctx, keys.Artist+" "+keys.Title, discovery.ResultKindTrack)
	if err != nil {
		return nil, err
	}
	discography := map[string]bool{}
	for _, album := range view.Albums {
		discography[catalogKey(album)] = true
	}
	var found []candidate
	for _, song := range results {
		if song.Kind != discovery.ResultKindTrack || len(song.Sources) == 0 || !discography[catalogKey(song.Album)] {
			continue
		}
		found = append(found, candidate{
			ID: song.Sources[0].ExternalID, ISRC: song.ISRC, Title: song.Title, Artists: []string{song.Subtitle},
			Album: song.Album, LengthSeconds: float64(song.Duration),
		})
	}
	return found, nil
}
